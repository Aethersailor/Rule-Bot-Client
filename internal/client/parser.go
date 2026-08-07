package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
)

const maxLogLineSize = 256 * 1024

var (
	errLineTooLong = errors.New("line exceeds configured limit")
	payloadKey     = []byte(`"payload"`)
	arrowMarker    = []byte(" --> ")
	successMarker  = []byte(" match Match using ")
	failureMarker  = []byte("(match Match/)")
	errorMarker    = []byte(" error:")
)

func parseLogLine(line []byte, includeFailed, includeSingleLabel bool) (string, bool, error) {
	line = bytes.TrimSpace(line)
	if len(line) < 2 || line[0] != '{' || line[len(line)-1] != '}' {
		return "", false, errors.New("log line is not a JSON object")
	}
	payload, err := jsonStringField(line, payloadKey)
	if err != nil {
		return "", false, err
	}
	return parseLogPayload(payload, includeFailed, includeSingleLabel)
}

func parseLogPayload(payload []byte, includeFailed, includeSingleLabel bool) (string, bool, error) {
	end := bytes.Index(payload, successMarker)
	if end < 0 {
		if !includeFailed || !bytes.Contains(payload, failureMarker) {
			return "", false, nil
		}
		end = bytes.Index(payload, errorMarker)
		if end < 0 {
			return "", false, nil
		}
	}
	arrow := bytes.LastIndex(payload[:end], arrowMarker)
	if arrow < 0 {
		return "", false, nil
	}
	target := strings.TrimSpace(string(payload[arrow+len(arrowMarker) : end]))
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return "", false, nil
	}
	domain, ok := normalizeDomain(host, includeSingleLabel)
	return domain, ok, nil
}

func jsonStringField(line, key []byte) ([]byte, error) {
	searchFrom := 0
	for {
		index := bytes.Index(line[searchFrom:], key)
		if index < 0 {
			return nil, errors.New("JSON payload field is missing")
		}
		index += searchFrom
		position := index + len(key)
		for position < len(line) && isJSONSpace(line[position]) {
			position++
		}
		if position >= len(line) || line[position] != ':' {
			searchFrom = index + len(key)
			continue
		}
		position++
		for position < len(line) && isJSONSpace(line[position]) {
			position++
		}
		if position >= len(line) || line[position] != '"' {
			return nil, errors.New("JSON payload field is not a string")
		}

		start := position + 1
		escaped := false
		for position = start; position < len(line); position++ {
			switch line[position] {
			case '\\':
				escaped = true
				position++
			case '"':
				if !escaped {
					return line[start:position], nil
				}
				var decoded string
				if err := json.Unmarshal(line[start-1:position+1], &decoded); err != nil {
					return nil, errors.New("invalid escaped JSON payload")
				}
				return []byte(decoded), nil
			}
		}
		return nil, errors.New("unterminated JSON payload string")
	}
}

func isJSONSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\r' || char == '\n'
}

func readBoundedLine(reader *bufio.Reader, scratch *[]byte, limit int) ([]byte, error) {
	*scratch = (*scratch)[:0]
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(*scratch) == 0 && err == nil {
			return trimLineEnding(fragment), nil
		}
		if len(*scratch)+len(fragment) > limit {
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = reader.ReadSlice('\n')
			}
			return nil, errLineTooLong
		}
		*scratch = append(*scratch, fragment...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return trimLineEnding(*scratch), err
	}
}

func trimLineEnding(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	return line
}

func consumeLogStream(reader io.Reader, includeFailed, includeSingleLabel bool, submit func(string) bool, malformed func(error)) error {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	scratch := make([]byte, 0, 64*1024)
	for {
		line, err := readBoundedLine(buffered, &scratch, maxLogLineSize)
		if len(line) != 0 {
			domain, ok, parseErr := parseLogLine(line, includeFailed, includeSingleLabel)
			if parseErr != nil {
				malformed(parseErr)
			} else if ok && !submit(domain) {
				return nil
			}
		}
		if errors.Is(err, errLineTooLong) {
			malformed(err)
			continue
		}
		if err != nil {
			return err
		}
	}
}
