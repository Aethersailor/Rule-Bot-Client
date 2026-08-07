package openwrt

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type UCISection struct {
	Type    string
	Name    string
	Options map[string]string
	Lists   map[string][]string
}

type UCIConfig struct {
	Sections []UCISection
}

func ParseUCI(data []byte) (UCIConfig, error) {
	var result UCIConfig
	var current *UCISection
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		words, err := splitUCIWords(scanner.Text())
		if err != nil {
			return UCIConfig{}, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		if len(words) == 0 {
			continue
		}
		switch words[0] {
		case "config":
			if len(words) < 2 || len(words) > 3 {
				return UCIConfig{}, fmt.Errorf("line %d: config requires type and optional name", lineNumber)
			}
			name := ""
			if len(words) == 3 {
				name = words[2]
			}
			result.Sections = append(result.Sections, UCISection{
				Type: words[1], Name: name, Options: map[string]string{}, Lists: map[string][]string{},
			})
			current = &result.Sections[len(result.Sections)-1]
		case "option", "list":
			if current == nil || len(words) != 3 {
				return UCIConfig{}, fmt.Errorf("line %d: %s requires an active section, key, and value", lineNumber, words[0])
			}
			if words[0] == "option" {
				current.Options[words[1]] = words[2]
			} else {
				current.Lists[words[1]] = append(current.Lists[words[1]], words[2])
			}
		default:
			return UCIConfig{}, fmt.Errorf("line %d: unsupported directive %q", lineNumber, words[0])
		}
	}
	if err := scanner.Err(); err != nil {
		return UCIConfig{}, err
	}
	return result, nil
}

func splitUCIWords(line string) ([]string, error) {
	var words []string
	for index := 0; index < len(line); {
		for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
			index++
		}
		if index == len(line) || line[index] == '#' {
			break
		}
		var word strings.Builder
		started := false
		for index < len(line) {
			char := line[index]
			if char == ' ' || char == '\t' || char == '#' {
				break
			}
			started = true
			switch char {
			case '\'', '"':
				quote := char
				index++
				closed := false
				for index < len(line) {
					if line[index] == quote {
						index++
						closed = true
						break
					}
					if quote == '"' && line[index] == '\\' && index+1 < len(line) {
						index++
					}
					word.WriteByte(line[index])
					index++
				}
				if !closed {
					return nil, errors.New("unterminated quoted value")
				}
			case '\\':
				index++
				if index >= len(line) {
					return nil, errors.New("trailing escape")
				}
				word.WriteByte(line[index])
				index++
			default:
				word.WriteByte(char)
				index++
			}
		}
		if started {
			words = append(words, word.String())
		}
		for index < len(line) && line[index] != '#' && (line[index] == ' ' || line[index] == '\t') {
			index++
		}
		if index < len(line) && line[index] == '#' {
			break
		}
	}
	return words, nil
}

func RenderUCI(config UCIConfig) []byte {
	var output strings.Builder
	for index, section := range config.Sections {
		if index != 0 {
			output.WriteByte('\n')
		}
		output.WriteString("config ")
		output.WriteString(quoteUCI(section.Type))
		if section.Name != "" {
			output.WriteByte(' ')
			output.WriteString(quoteUCI(section.Name))
		}
		output.WriteByte('\n')
		keys := make([]string, 0, len(section.Options))
		for key := range section.Options {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			output.WriteString("\toption ")
			output.WriteString(quoteUCI(key))
			output.WriteByte(' ')
			output.WriteString(quoteUCI(section.Options[key]))
			output.WriteByte('\n')
		}
		listKeys := make([]string, 0, len(section.Lists))
		for key := range section.Lists {
			listKeys = append(listKeys, key)
		}
		sort.Strings(listKeys)
		for _, key := range listKeys {
			for _, value := range section.Lists[key] {
				output.WriteString("\tlist ")
				output.WriteString(quoteUCI(key))
				output.WriteByte(' ')
				output.WriteString(quoteUCI(value))
				output.WriteByte('\n')
			}
		}
	}
	return []byte(output.String())
}

func quoteUCI(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func loadUCI(path string) (UCIConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return UCIConfig{}, err
	}
	return ParseUCI(data)
}

func (c UCIConfig) section(sectionType, name string) *UCISection {
	for index := range c.Sections {
		section := &c.Sections[index]
		if section.Type == sectionType && (name == "" || section.Name == name) {
			return section
		}
	}
	return nil
}

func boolOption(section *UCISection, key string, fallback bool) bool {
	if section == nil {
		return fallback
	}
	value, exists := section.Options[key]
	if !exists {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func intOption(section *UCISection, key string) int {
	if section == nil {
		return 0
	}
	value, _ := strconv.Atoi(strings.TrimSpace(section.Options[key]))
	return value
}

func rooted(root, path string) string {
	if root == "" || root == "/" {
		return path
	}
	return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, "/")))
}
