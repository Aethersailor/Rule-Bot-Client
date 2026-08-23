package client

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultUpdateManifestURL = "https://github.com/Aethersailor/Rule-Bot-Client/releases/latest/download/client-update-manifest.json"
	defaultReleaseBaseURL    = "https://github.com/Aethersailor/Rule-Bot-Client/releases/download/"
	maxClientManifestBytes   = 1024 * 1024
	maxClientAssetBytes      = 128 * 1024 * 1024
)

var (
	ErrNoUpdate         = errors.New("already running the latest stable version")
	ErrRestartScheduled = errors.New("update replacement has been scheduled")
	stableUpdateVersion = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	buildUpdateVersion  = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:[_-][0-9A-Za-z.-]+)?$`)
	updateTargetPattern = regexp.MustCompile(`^(?:linux|windows)-(?:amd64|386|arm64|armv[5-7]|mips(?:le)?-(?:softfloat|hardfloat)|mips64(?:le)?|riscv64)$`)
	clientAssetPattern  = regexp.MustCompile(`^rule-bot-client[-_+.0-9A-Za-z]+\.(?:tar\.gz|zip|deb)$`)
	clientSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type clientUpdateVersion struct{ major, minor, patch int }

type clientUpdateManifest struct {
	Schema  int                 `json:"schema"`
	Version string              `json:"version"`
	Commit  string              `json:"commit"`
	Assets  []ClientUpdateAsset `json:"assets"`
}

type ClientUpdateAsset struct {
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type ClientUpdateInfo struct {
	CurrentVersion string            `json:"current_version"`
	LatestVersion  string            `json:"latest_version"`
	Commit         string            `json:"commit"`
	Target         string            `json:"target"`
	Kind           string            `json:"kind"`
	Asset          ClientUpdateAsset `json:"asset"`
}

type ClientUpdateOptions struct {
	ConfigPath     string
	Executable     string
	RestartArgs    []string
	ManifestURL    string
	ReleaseBaseURL string
	HTTPClient     *http.Client
}

type PreparedClientUpdate struct {
	Info         ClientUpdateInfo
	options      ClientUpdateOptions
	workDir      string
	downloadPath string
	candidate    string
	rollback     string
}

func parseClientUpdateVersion(raw string, stableOnly bool) (clientUpdateVersion, error) {
	pattern := buildUpdateVersion
	if stableOnly {
		pattern = stableUpdateVersion
	}
	match := pattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return clientUpdateVersion{}, fmt.Errorf("unsupported update version %q", raw)
	}
	values := [3]int{}
	for index := range values {
		value, err := strconv.Atoi(match[index+1])
		if err != nil {
			return clientUpdateVersion{}, err
		}
		values[index] = value
	}
	return clientUpdateVersion{values[0], values[1], values[2]}, nil
}

func compareClientUpdateVersions(left, right clientUpdateVersion) int {
	for _, pair := range [][2]int{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}

func normalizedClientUpdateOptions(options ClientUpdateOptions) (ClientUpdateOptions, error) {
	if options.Executable == "" {
		executable, err := os.Executable()
		if err != nil {
			return options, err
		}
		options.Executable = executable
	}
	absolute, err := filepath.Abs(options.Executable)
	if err != nil {
		return options, err
	}
	options.Executable = filepath.Clean(absolute)
	if options.ManifestURL == "" {
		options.ManifestURL = defaultUpdateManifestURL
	}
	if options.ReleaseBaseURL == "" {
		options.ReleaseBaseURL = defaultReleaseBaseURL
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{
			Timeout: 45 * time.Second,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) >= 6 {
					return errors.New("too many update redirects")
				}
				if request.URL.Scheme != "https" {
					return errors.New("update redirected to a non-HTTPS URL")
				}
				return nil
			},
		}
	}
	return options, nil
}

func fetchClientUpdateManifest(ctx context.Context, target string, options ClientUpdateOptions) (clientUpdateManifest, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return clientUpdateManifest{}, err
	}
	request.Header.Set("User-Agent", "Rule-Bot-Client-Updater")
	request.Header.Set("Accept", "application/json")
	response, err := options.HTTPClient.Do(request)
	if err != nil {
		return clientUpdateManifest{}, fmt.Errorf("download update manifest: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return clientUpdateManifest{}, fmt.Errorf("download update manifest: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxClientManifestBytes+1))
	if err != nil {
		return clientUpdateManifest{}, err
	}
	if len(data) > maxClientManifestBytes {
		return clientUpdateManifest{}, errors.New("update manifest exceeds 1 MiB")
	}
	var manifest clientUpdateManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return clientUpdateManifest{}, fmt.Errorf("decode update manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return clientUpdateManifest{}, errors.New("update manifest contains trailing data")
	}
	if manifest.Schema != 1 || stableUpdateVersion.FindStringSubmatch(manifest.Version) == nil {
		return clientUpdateManifest{}, errors.New("update manifest identity is invalid")
	}
	if matched, _ := regexp.MatchString(`^[0-9a-f]{40}$`, manifest.Commit); !matched {
		return clientUpdateManifest{}, errors.New("update manifest commit is invalid")
	}
	seen := map[string]struct{}{}
	if len(manifest.Assets) == 0 {
		return clientUpdateManifest{}, errors.New("update manifest contains no assets")
	}
	for _, asset := range manifest.Assets {
		key := asset.Target + ":" + asset.Kind
		if !updateTargetPattern.MatchString(asset.Target) || (asset.Kind != "archive" && asset.Kind != "deb") ||
			!clientAssetPattern.MatchString(asset.Name) || !clientSHA256Pattern.MatchString(asset.SHA256) ||
			asset.Size <= 0 || asset.Size > maxClientAssetBytes {
			return clientUpdateManifest{}, fmt.Errorf("update manifest contains invalid asset %q", asset.Name)
		}
		if _, exists := seen[key]; exists {
			return clientUpdateManifest{}, fmt.Errorf("update manifest contains duplicate target %s", key)
		}
		seen[key] = struct{}{}
	}
	return manifest, nil
}

func selectClientUpdateAsset(manifest clientUpdateManifest, target, kind string) (ClientUpdateAsset, error) {
	selected := []ClientUpdateAsset{}
	for _, asset := range manifest.Assets {
		if asset.Target == target && asset.Kind == kind {
			selected = append(selected, asset)
		}
	}
	if len(selected) != 1 {
		return ClientUpdateAsset{}, fmt.Errorf("update manifest has %d assets for %s:%s", len(selected), target, kind)
	}
	return selected[0], nil
}

func CheckClientUpdate(ctx context.Context, options ClientUpdateOptions) (ClientUpdateInfo, error) {
	options, err := normalizedClientUpdateOptions(options)
	if err != nil {
		return ClientUpdateInfo{}, err
	}
	if !updateTargetPattern.MatchString(BuildTarget) {
		return ClientUpdateInfo{}, fmt.Errorf("build target %q cannot use automatic updates", BuildTarget)
	}
	manifest, err := fetchClientUpdateManifest(ctx, options.ManifestURL, options)
	if err != nil {
		return ClientUpdateInfo{}, err
	}
	current, err := parseClientUpdateVersion(BuildVersion, false)
	if err != nil {
		return ClientUpdateInfo{}, err
	}
	latest, err := parseClientUpdateVersion(manifest.Version, true)
	if err != nil {
		return ClientUpdateInfo{}, err
	}
	kind := platformClientUpdateKind(options.Executable)
	asset, err := selectClientUpdateAsset(manifest, BuildTarget, kind)
	if err != nil {
		return ClientUpdateInfo{}, err
	}
	info := ClientUpdateInfo{
		CurrentVersion: BuildVersion, LatestVersion: manifest.Version, Commit: manifest.Commit,
		Target: BuildTarget, Kind: kind, Asset: asset,
	}
	if compareClientUpdateVersions(current, latest) >= 0 {
		return info, ErrNoUpdate
	}
	return info, nil
}

func clientUpdateAssetURL(options ClientUpdateOptions, version, name string) (string, error) {
	base, err := url.Parse(options.ReleaseBaseURL)
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + "/" + version + "/" + name
	return base.String(), nil
}

func clientUpdateManifestURL(options ClientUpdateOptions, version string) (string, error) {
	return clientUpdateAssetURL(options, version, "client-update-manifest.json")
}

func downloadClientUpdateAsset(ctx context.Context, options ClientUpdateOptions, version string, asset ClientUpdateAsset, destination string) error {
	target, err := clientUpdateAssetURL(options, version, asset.Name)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "Rule-Bot-Client-Updater")
	response, err := options.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("download update asset: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download update asset: HTTP %d", response.StatusCode)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxClientAssetBytes+1))
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	if written != asset.Size {
		return fmt.Errorf("update asset size mismatch: expected %d, got %d", asset.Size, written)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != asset.SHA256 {
		return errors.New("update asset SHA256 mismatch")
	}
	return nil
}

func extractClientUpdateBinary(archivePath, target, destination string) error {
	windows := strings.HasPrefix(target, "windows-")
	wanted := "rule-bot-client"
	if windows {
		wanted += ".exe"
		reader, err := zip.OpenReader(archivePath)
		if err != nil {
			return err
		}
		defer reader.Close()
		for _, entry := range reader.File {
			if filepath.Base(entry.Name) != wanted || entry.FileInfo().IsDir() {
				continue
			}
			input, err := entry.Open()
			if err != nil {
				return err
			}
			err = writeCandidateBinary(input, destination)
			_ = input.Close()
			return err
		}
		return fmt.Errorf("Windows archive does not contain %s", wanted)
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == wanted {
			return writeCandidateBinary(tarReader, destination)
		}
	}
	return fmt.Errorf("Linux archive does not contain %s", wanted)
}

func writeCandidateBinary(reader io.Reader, destination string) error {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(reader, 16*1024*1024+1))
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	if written <= 0 || written > 16*1024*1024 {
		return errors.New("candidate executable size is invalid")
	}
	return nil
}

func verifyClientUpdateCandidate(ctx context.Context, candidate string, info ClientUpdateInfo, configPath string) error {
	output, err := exec.CommandContext(ctx, candidate, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("run candidate version check: %w", err)
	}
	identity := string(output)
	if !strings.Contains(identity, "rule-bot-client "+info.LatestVersion+" ") ||
		!strings.Contains(identity, "commit="+info.Commit+" ") || !strings.Contains(identity, "target="+info.Target) {
		return fmt.Errorf("candidate identity mismatch: %s", strings.TrimSpace(identity))
	}
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			output, err = exec.CommandContext(ctx, candidate, "--config", configPath, "--check").CombinedOutput()
			if err != nil {
				return fmt.Errorf("candidate configuration check failed: %s", strings.TrimSpace(string(output)))
			}
		}
	}
	return nil
}

func PrepareClientUpdate(ctx context.Context, options ClientUpdateOptions) (*PreparedClientUpdate, error) {
	options, err := normalizedClientUpdateOptions(options)
	if err != nil {
		return nil, err
	}
	info, err := CheckClientUpdate(ctx, options)
	if err != nil {
		return nil, err
	}
	workDir, err := os.MkdirTemp("", "rule-bot-client-update-*")
	if err != nil {
		return nil, err
	}
	prepared := &PreparedClientUpdate{Info: info, options: options, workDir: workDir}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(workDir)
		}
	}()
	prepared.downloadPath = filepath.Join(workDir, info.Asset.Name)
	if err := downloadClientUpdateAsset(ctx, options, info.LatestVersion, info.Asset, prepared.downloadPath); err != nil {
		return nil, err
	}
	if err := preparePlatformClientUpdate(ctx, prepared); err != nil {
		return nil, err
	}
	if err := verifyClientUpdateCandidate(ctx, prepared.candidate, info, options.ConfigPath); err != nil {
		return nil, err
	}
	cleanup = false
	return prepared, nil
}

func (prepared *PreparedClientUpdate) Abort() {
	if prepared != nil && prepared.workDir != "" {
		_ = os.RemoveAll(prepared.workDir)
	}
}

func (prepared *PreparedClientUpdate) Activate(ctx context.Context) error {
	if prepared == nil {
		return errors.New("update candidate is missing")
	}
	return activatePlatformClientUpdate(ctx, prepared)
}

func ApplyLatestClientUpdate(ctx context.Context, options ClientUpdateOptions) (ClientUpdateInfo, error) {
	prepared, err := PrepareClientUpdate(ctx, options)
	if err != nil {
		return ClientUpdateInfo{}, err
	}
	defer prepared.Abort()
	return prepared.Info, prepared.Activate(ctx)
}
