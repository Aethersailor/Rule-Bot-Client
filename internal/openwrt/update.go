package openwrt

import (
	"bufio"
	"bytes"
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
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Aethersailor/Rule-Bot-Client/internal/client"
)

const (
	latestOpenWrtManifestURL = "https://github.com/Aethersailor/Rule-Bot-Client/releases/latest/download/openwrt-manifest.tsv"
	openWrtReleaseBaseURL    = "https://github.com/Aethersailor/Rule-Bot-Client/releases/download/"
	maxUpdateManifestBytes   = 128 * 1024
	maxUpdatePackageBytes    = 32 * 1024 * 1024
	updateSpaceMargin        = 4 * 1024 * 1024
	updateLockMaxAge         = 30 * time.Minute
)

var (
	releaseManifestPathPattern = regexp.MustCompile(`^/Aethersailor/Rule-Bot-Client/releases/download/(v[0-9]+\.[0-9]+\.[0-9]+)/openwrt-manifest\.tsv$`)
	stableVersionPattern       = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	buildVersionPattern        = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:_git[0-9]+)?$`)
	updateAssetPattern         = regexp.MustCompile(`^luci-app-rule-bot-client[-_+.0-9A-Za-z]+\.(?:ipk|apk)$`)
	updateArchitecturePattern  = regexp.MustCompile(`^[0-9A-Za-z_+-]+$`)
	updateSHA256Pattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type updateVersion struct {
	major int
	minor int
	patch int
}

type updatePackage struct {
	Format       string
	Architecture string
	Asset        string
	SHA256       string
	Size         int64
	SDKURL       string
}

type updateEnvironment struct {
	Manager       string
	Format        string
	Architectures map[string]int
}

type UpdateInfo struct {
	CurrentVersion       string `json:"current_version"`
	LatestVersion        string `json:"latest_version"`
	Tag                  string `json:"tag"`
	Available            bool   `json:"available"`
	PackageManager       string `json:"package_manager"`
	PackageFormat        string `json:"package_format"`
	Architecture         string `json:"architecture"`
	Asset                string `json:"asset"`
	Size                 int64  `json:"size"`
	SDKURL               string `json:"sdk_url"`
	CompatibilityWarning string `json:"compatibility_warning,omitempty"`
}

type UpdateState struct {
	State     string      `json:"state"`
	Info      *UpdateInfo `json:"info,omitempty"`
	StartedAt time.Time   `json:"started_at,omitempty"`
	UpdatedAt time.Time   `json:"updated_at"`
	ErrorCode string      `json:"error_code,omitempty"`
	Error     string      `json:"error,omitempty"`
}

func parseUpdateVersion(raw string, stableOnly bool) (updateVersion, error) {
	pattern := buildVersionPattern
	if stableOnly {
		pattern = stableVersionPattern
	}
	match := pattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return updateVersion{}, fmt.Errorf("unsupported version %q", raw)
	}
	values := [3]int{}
	for index := range values {
		value, err := strconv.Atoi(match[index+1])
		if err != nil {
			return updateVersion{}, fmt.Errorf("parse version %q: %w", raw, err)
		}
		values[index] = value
	}
	return updateVersion{major: values[0], minor: values[1], patch: values[2]}, nil
}

func compareUpdateVersions(left, right updateVersion) int {
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

func parseUpdateManifest(data []byte) ([]updatePackage, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), maxUpdateManifestBytes)
	line := 0
	packages := []updatePackage{}
	seen := map[string]struct{}{}
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		fields := strings.Split(text, "\t")
		if line == 1 {
			if text != "format\tarchitecture\tasset\tsha256\tsize\tsdk_url" {
				return nil, errors.New("update manifest header is invalid")
			}
			continue
		}
		if len(fields) != 6 {
			return nil, fmt.Errorf("update manifest line %d must contain six fields", line)
		}
		format, architecture, asset := fields[0], fields[1], fields[2]
		if format != "ipk" && format != "apk" {
			return nil, fmt.Errorf("update manifest line %d has an unsupported package format", line)
		}
		if !updateArchitecturePattern.MatchString(architecture) || !updateAssetPattern.MatchString(asset) {
			return nil, fmt.Errorf("update manifest line %d has an unsafe package identity", line)
		}
		if (format == "ipk" && !strings.HasSuffix(asset, ".ipk")) || (format == "apk" && !strings.HasSuffix(asset, ".apk")) {
			return nil, fmt.Errorf("update manifest line %d has a mismatched package extension", line)
		}
		if !updateSHA256Pattern.MatchString(fields[3]) {
			return nil, fmt.Errorf("update manifest line %d has an invalid SHA256", line)
		}
		size, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil || size <= 0 || size > maxUpdatePackageBytes {
			return nil, fmt.Errorf("update manifest line %d has an invalid package size", line)
		}
		if !strings.HasPrefix(fields[5], "https://downloads.openwrt.org/releases/") {
			return nil, fmt.Errorf("update manifest line %d has an unexpected SDK identity", line)
		}
		key := format + ":" + architecture
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("update manifest contains duplicate package identity %s", key)
		}
		seen[key] = struct{}{}
		packages = append(packages, updatePackage{
			Format: format, Architecture: architecture, Asset: asset,
			SHA256: fields[3], Size: size, SDKURL: fields[5],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read update manifest: %w", err)
	}
	if len(packages) == 0 {
		return nil, errors.New("update manifest contains no packages")
	}
	return packages, nil
}

func selectUpdatePackage(packages []updatePackage, environment updateEnvironment) (updatePackage, error) {
	selected := updatePackage{}
	best := -1
	matches := 0
	for _, candidate := range packages {
		priority, supported := environment.Architectures[candidate.Architecture]
		if candidate.Format != environment.Format || !supported {
			continue
		}
		if priority > best {
			selected = candidate
			best = priority
			matches = 1
		} else if priority == best {
			matches++
		}
	}
	if best < 0 {
		return updatePackage{}, fmt.Errorf("latest Release has no package for %s and the detected architecture", environment.Manager)
	}
	if matches != 1 {
		return updatePackage{}, errors.New("latest Release does not contain a unique matching package")
	}
	return selected, nil
}

func releaseTagFromManifestURL(value string, testing bool) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", errors.New("latest Release returned an invalid manifest URL")
	}
	if !testing && (parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com")) {
		return "", errors.New("latest Release redirected outside the expected GitHub repository")
	}
	match := releaseManifestPathPattern.FindStringSubmatch(parsed.Path)
	if match == nil {
		return "", errors.New("latest Release redirect does not identify a stable version")
	}
	return match[1], nil
}

func (b Backend) updateManifestURL() string {
	if b.Testing && b.UpdateManifestURL != "" {
		return b.UpdateManifestURL
	}
	return latestOpenWrtManifestURL
}

func (b Backend) updateHTTPClient(followRedirects bool) *http.Client {
	client := &http.Client{Timeout: 30 * time.Second}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if !followRedirects {
			return http.ErrUseLastResponse
		}
		if len(via) >= 6 {
			return errors.New("too many update download redirects")
		}
		if !b.Testing && request.URL.Scheme != "https" {
			return errors.New("update download redirected to a non-HTTPS URL")
		}
		return nil
	}
	return client
}

func newUpdateRequest(ctx context.Context, method, target string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Rule-Bot-Client-OpenWrt-Updater")
	request.Header.Set("Accept", "application/octet-stream")
	return request, nil
}

func (b Backend) resolveLatestManifest(ctx context.Context) (string, string, error) {
	request, err := newUpdateRequest(ctx, http.MethodHead, b.updateManifestURL())
	if err != nil {
		return "", "", err
	}
	response, err := b.updateHTTPClient(false).Do(request)
	if err != nil {
		return "", "", fmt.Errorf("check latest Release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 300 || response.StatusCode >= 400 {
		return "", "", fmt.Errorf("check latest Release: unexpected HTTP status %d", response.StatusCode)
	}
	location, err := response.Location()
	if err != nil {
		return "", "", errors.New("latest Release did not return a download location")
	}
	location = response.Request.URL.ResolveReference(location)
	tag, err := releaseTagFromManifestURL(location.String(), b.Testing)
	if err != nil {
		return "", "", err
	}
	return tag, location.String(), nil
}

func (b Backend) fetchUpdateManifest(ctx context.Context, target string) ([]updatePackage, error) {
	request, err := newUpdateRequest(ctx, http.MethodGet, target)
	if err != nil {
		return nil, err
	}
	response, err := b.updateHTTPClient(true).Do(request)
	if err != nil {
		return nil, fmt.Errorf("download update manifest: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download update manifest: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxUpdateManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read update manifest: %w", err)
	}
	if len(data) > maxUpdateManifestBytes {
		return nil, errors.New("update manifest exceeds 128 KiB")
	}
	return parseUpdateManifest(data)
}

func (b Backend) runUpdateCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	if b.Testing {
		return nil, errors.New("update commands are unavailable in an offline test root")
	}
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if len(output) > 16*1024 {
		output = output[len(output)-(16*1024):]
	}
	if err != nil {
		return output, fmt.Errorf("%s failed: %s", filepath.Base(name), strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (b Backend) detectUpdateEnvironment(ctx context.Context) (updateEnvironment, error) {
	if b.Root != "" && b.Root != "/" {
		return updateEnvironment{}, errors.New("update detection is unavailable in an offline test root")
	}
	if _, err := os.Stat("/usr/bin/apk"); err == nil {
		output, err := b.runUpdateCommand(ctx, "/usr/bin/apk", "--print-arch")
		if err != nil {
			return updateEnvironment{}, err
		}
		architecture := strings.TrimSpace(string(output))
		if !updateArchitecturePattern.MatchString(architecture) {
			return updateEnvironment{}, errors.New("apk returned an invalid architecture")
		}
		return updateEnvironment{Manager: "apk", Format: "apk", Architectures: map[string]int{architecture: 1}}, nil
	}
	if _, err := os.Stat("/bin/opkg"); err == nil {
		output, err := b.runUpdateCommand(ctx, "/bin/opkg", "print-architecture")
		if err != nil {
			return updateEnvironment{}, err
		}
		architectures := map[string]int{}
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 3 || fields[0] != "arch" || !updateArchitecturePattern.MatchString(fields[1]) {
				continue
			}
			priority, err := strconv.Atoi(fields[2])
			if err == nil {
				architectures[fields[1]] = priority
			}
		}
		if len(architectures) == 0 {
			return updateEnvironment{}, errors.New("opkg returned no supported architectures")
		}
		return updateEnvironment{Manager: "opkg", Format: "ipk", Architectures: architectures}, nil
	}
	return updateEnvironment{}, errors.New("neither apk nor opkg is available")
}

func readOpenWrtRelease(root string) string {
	data, err := os.ReadFile(rooted(root, "/etc/openwrt_release"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "DISTRIB_RELEASE=") {
			continue
		}
		return strings.Trim(strings.TrimPrefix(line, "DISTRIB_RELEASE="), "'\"")
	}
	return ""
}

func updateCompatibilityWarning(root, sdkURL string) (string, error) {
	detected := readOpenWrtRelease(root)
	match := regexp.MustCompile(`/releases/([^/]+)/`).FindStringSubmatch(sdkURL)
	if detected == "" || match == nil {
		return "", nil
	}
	detectedSeries := regexp.MustCompile(`^[0-9]+\.[0-9]+`).FindString(detected)
	sdkSeries := regexp.MustCompile(`^[0-9]+\.[0-9]+`).FindString(match[1])
	if detectedSeries != "" && sdkSeries != "" && detectedSeries != sdkSeries {
		return "", fmt.Errorf("package was built for OpenWrt %s but this device reports %s", sdkSeries, detected)
	}
	if detectedSeries == "" {
		return fmt.Sprintf("This device reports %s; compatibility is determined by package manager and architecture.", detected), nil
	}
	return "", nil
}

func (b Backend) resolveUpdate(ctx context.Context) (UpdateInfo, updateEnvironment, updatePackage, error) {
	environment, err := b.detectUpdateEnvironment(ctx)
	if err != nil {
		return UpdateInfo{}, updateEnvironment{}, updatePackage{}, err
	}
	tag, manifestURL, err := b.resolveLatestManifest(ctx)
	if err != nil {
		return UpdateInfo{}, updateEnvironment{}, updatePackage{}, err
	}
	packages, err := b.fetchUpdateManifest(ctx, manifestURL)
	if err != nil {
		return UpdateInfo{}, updateEnvironment{}, updatePackage{}, err
	}
	selected, err := selectUpdatePackage(packages, environment)
	if err != nil {
		return UpdateInfo{}, updateEnvironment{}, updatePackage{}, err
	}
	warning, err := updateCompatibilityWarning(b.Root, selected.SDKURL)
	if err != nil {
		return UpdateInfo{}, updateEnvironment{}, updatePackage{}, err
	}
	current, err := parseUpdateVersion(client.BuildVersion, false)
	if err != nil {
		return UpdateInfo{}, updateEnvironment{}, updatePackage{}, fmt.Errorf("current build cannot use automatic updates: %w", err)
	}
	latest, err := parseUpdateVersion(tag, true)
	if err != nil {
		return UpdateInfo{}, updateEnvironment{}, updatePackage{}, err
	}
	latestText := strings.TrimPrefix(tag, "v")
	info := UpdateInfo{
		CurrentVersion: client.BuildVersion, LatestVersion: latestText, Tag: tag,
		Available:      compareUpdateVersions(current, latest) < 0,
		PackageManager: environment.Manager, PackageFormat: environment.Format,
		Architecture: selected.Architecture, Asset: selected.Asset, Size: selected.Size,
		SDKURL: selected.SDKURL, CompatibilityWarning: warning,
	}
	return info, environment, selected, nil
}

func (b Backend) updateStatusPath() string {
	return rooted(b.Root, "/var/run/rule-bot-client/update-status.json")
}

func (b Backend) writeUpdateState(state UpdateState) error {
	state.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicWrite(b.updateStatusPath(), append(data, '\n'), 0o600)
}

func (b Backend) updateStatus() (UpdateState, error) {
	data, err := os.ReadFile(b.updateStatusPath())
	if errors.Is(err, os.ErrNotExist) {
		return UpdateState{State: "idle", UpdatedAt: time.Now().UTC()}, nil
	}
	if err != nil {
		return UpdateState{}, err
	}
	if len(data) > 64*1024 {
		return UpdateState{}, errors.New("update status exceeds 64 KiB")
	}
	var state UpdateState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return UpdateState{}, fmt.Errorf("decode update status: %w", err)
	}
	return state, nil
}

func (b Backend) checkUpdate(ctx context.Context) (UpdateInfo, error) {
	started := time.Now().UTC()
	_ = b.writeUpdateState(UpdateState{State: "checking", StartedAt: started})
	info, _, _, err := b.resolveUpdate(ctx)
	if err != nil {
		_ = b.writeUpdateState(UpdateState{State: "failed", StartedAt: started, ErrorCode: "update_check_failed", Error: err.Error()})
		return UpdateInfo{}, err
	}
	state := "up_to_date"
	if info.Available {
		state = "available"
	}
	_ = b.writeUpdateState(UpdateState{State: state, StartedAt: started, Info: &info})
	return info, nil
}

func (b Backend) setAutomaticUpdates(enabled bool) (map[string]any, error) {
	settings, err := LoadSettings(b.Root)
	if err != nil {
		return nil, err
	}
	settings.AutoUpdate = enabled
	path := rooted(b.Root, "/etc/config/rule_bot_client")
	if err := atomicWrite(path, RenderUCI(settingsUCI(settings)), 0o600); err != nil {
		return nil, fmt.Errorf("save automatic update setting: %w", err)
	}
	return map[string]any{"ok": true, "enabled": enabled}, nil
}

func (b Backend) acquireUpdateLock() (func(), error) {
	lockPath := rooted(b.Root, "/var/run/rule-bot-client/update.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o750); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, writeErr := fmt.Fprintf(file, "%d\n", os.Getpid())
			closeErr := file.Close()
			if joined := errors.Join(writeErr, closeErr); joined != nil {
				_ = os.Remove(lockPath)
				return nil, joined
			}
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, statErr := os.Stat(lockPath)
		if statErr != nil || time.Since(info.ModTime()) <= updateLockMaxAge {
			return nil, errors.New("an update is already in progress")
		}
		_ = os.Remove(lockPath)
	}
	return nil, errors.New("an update is already in progress")
}

func (b Backend) exactManifestURL(tag string) string {
	if b.Testing && b.UpdateManifestURL != "" {
		latest, _ := url.Parse(b.UpdateManifestURL)
		latest.Path = "/Aethersailor/Rule-Bot-Client/releases/download/" + tag + "/openwrt-manifest.tsv"
		latest.RawQuery = ""
		latest.Fragment = ""
		return latest.String()
	}
	return openWrtReleaseBaseURL + tag + "/openwrt-manifest.tsv"
}

func (b Backend) packageURL(tag, asset string) string {
	manifestURL, _ := url.Parse(b.exactManifestURL(tag))
	manifestURL.Path = path.Join(path.Dir(manifestURL.Path), asset)
	return manifestURL.String()
}

func (b Backend) downloadPackage(ctx context.Context, tag string, item updatePackage, destination string) error {
	request, err := newUpdateRequest(ctx, http.MethodGet, b.packageURL(tag, item.Asset))
	if err != nil {
		return err
	}
	response, err := b.updateHTTPClient(true).Do(request)
	if err != nil {
		return fmt.Errorf("download package: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download package: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxUpdatePackageBytes || response.ContentLength > item.Size {
		return errors.New("downloaded package exceeds the expected size")
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxUpdatePackageBytes+1))
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	if written != item.Size {
		return fmt.Errorf("package size mismatch: expected %d, got %d", item.Size, written)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != item.SHA256 {
		return errors.New("package SHA256 mismatch")
	}
	return nil
}

func stableCurrentTag(raw string) (string, bool) {
	if !stableVersionPattern.MatchString(raw) {
		return "", false
	}
	return "v" + strings.TrimPrefix(raw, "v"), true
}

func (b Backend) rollbackPackage(ctx context.Context, environment updateEnvironment, currentTag string) (updatePackage, error) {
	packages, err := b.fetchUpdateManifest(ctx, b.exactManifestURL(currentTag))
	if err != nil {
		return updatePackage{}, fmt.Errorf("prepare rollback manifest: %w", err)
	}
	item, err := selectUpdatePackage(packages, environment)
	if err != nil {
		return updatePackage{}, fmt.Errorf("prepare rollback package: %w", err)
	}
	return item, nil
}

func (b Backend) ensureUpdateSpace(newSize, oldSize int64) error {
	if b.Testing {
		return nil
	}
	temporaryRequired := uint64(newSize + oldSize + updateSpaceMargin)
	if available := availableBytes("/tmp"); available < temporaryRequired {
		return fmt.Errorf("insufficient /tmp space: need %d bytes, have %d", temporaryRequired, available)
	}
	rootRequired := uint64(newSize*4 + updateSpaceMargin)
	if available := availableBytes("/"); available < rootRequired {
		return fmt.Errorf("insufficient writable filesystem space: need %d bytes, have %d", rootRequired, available)
	}
	return nil
}

func (b Backend) installLocalPackage(ctx context.Context, environment updateEnvironment, filename string, rollback bool) error {
	switch environment.Manager {
	case "apk":
		_, err := b.runUpdateCommand(ctx, "/usr/bin/apk", "add", "--allow-untrusted", "--network=false", "--force-reinstall", filename)
		return err
	case "opkg":
		args := []string{"install"}
		if rollback {
			args = append(args, "--force-downgrade")
		}
		args = append(args, filename)
		_, err := b.runUpdateCommand(ctx, "/bin/opkg", args...)
		return err
	default:
		return errors.New("unsupported package manager")
	}
}

func (b Backend) verifyInstalledVersion(ctx context.Context, version string) error {
	for _, executable := range []string{"/usr/bin/rule-bot-client", "/usr/libexec/rule-bot-client-openwrt"} {
		output, err := b.runUpdateCommand(ctx, executable, "--version")
		if err != nil {
			return err
		}
		if !strings.Contains(string(output), "rule-bot-client "+version+" ") {
			return fmt.Errorf("%s did not report version %s", filepath.Base(executable), version)
		}
	}
	return nil
}

func (b Backend) runUpdate(ctx context.Context) error {
	release, err := b.acquireUpdateLock()
	if err != nil {
		return err
	}
	defer release()
	started := time.Now().UTC()
	_ = b.writeUpdateState(UpdateState{State: "checking", StartedAt: started})
	info, environment, target, err := b.resolveUpdate(ctx)
	if err != nil {
		_ = b.writeUpdateState(UpdateState{State: "failed", StartedAt: started, ErrorCode: "update_check_failed", Error: err.Error()})
		return err
	}
	if !info.Available {
		_ = b.writeUpdateState(UpdateState{State: "up_to_date", StartedAt: started, Info: &info})
		return nil
	}
	settings, err := LoadSettings(b.Root)
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("/tmp", "rule-bot-client-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	var rollback updatePackage
	rollbackTag, hasRollback := stableCurrentTag(client.BuildVersion)
	if hasRollback {
		rollback, err = b.rollbackPackage(ctx, environment, rollbackTag)
		if err != nil {
			_ = b.writeUpdateState(UpdateState{State: "failed", StartedAt: started, Info: &info, ErrorCode: "rollback_unavailable", Error: err.Error()})
			return err
		}
	}
	if err := b.ensureUpdateSpace(target.Size, rollback.Size); err != nil {
		_ = b.writeUpdateState(UpdateState{State: "failed", StartedAt: started, Info: &info, ErrorCode: "insufficient_space", Error: err.Error()})
		return err
	}
	_ = b.writeUpdateState(UpdateState{State: "downloading", StartedAt: started, Info: &info})
	targetPath := filepath.Join(work, "target-"+target.Asset)
	if err := b.downloadPackage(ctx, info.Tag, target, targetPath); err != nil {
		_ = b.writeUpdateState(UpdateState{State: "failed", StartedAt: started, Info: &info, ErrorCode: "package_download_failed", Error: err.Error()})
		return err
	}
	rollbackPath := ""
	if hasRollback {
		rollbackPath = filepath.Join(work, "rollback-"+rollback.Asset)
		if err := b.downloadPackage(ctx, rollbackTag, rollback, rollbackPath); err != nil {
			_ = b.writeUpdateState(UpdateState{State: "failed", StartedAt: started, Info: &info, ErrorCode: "rollback_download_failed", Error: err.Error()})
			return err
		}
	}
	_ = b.writeUpdateState(UpdateState{State: "installing", StartedAt: started, Info: &info})
	startedAfter := time.Now().UTC().Add(-time.Second)
	installErr := b.installLocalPackage(ctx, environment, targetPath, false)
	if installErr == nil {
		installErr = b.verifyInstalledVersion(ctx, info.LatestVersion)
	}
	if installErr == nil && settings.Enabled {
		installErr = b.confirmHealthy(startedAfter)
	}
	if installErr == nil {
		_ = b.writeUpdateState(UpdateState{State: "completed", StartedAt: started, Info: &info})
		return nil
	}
	if !hasRollback {
		_ = b.writeUpdateState(UpdateState{State: "failed", StartedAt: started, Info: &info, ErrorCode: "update_verification_failed", Error: installErr.Error()})
		return installErr
	}
	_ = b.writeUpdateState(UpdateState{State: "rolling_back", StartedAt: started, Info: &info, ErrorCode: "update_verification_failed", Error: installErr.Error()})
	rollbackStarted := time.Now().UTC().Add(-time.Second)
	rollbackErr := b.installLocalPackage(ctx, environment, rollbackPath, true)
	if rollbackErr == nil {
		rollbackErr = b.verifyInstalledVersion(ctx, strings.TrimPrefix(rollbackTag, "v"))
	}
	if rollbackErr == nil && settings.Enabled {
		rollbackErr = b.confirmHealthy(rollbackStarted)
	}
	if rollbackErr != nil {
		combined := fmt.Errorf("update failed: %v; rollback failed: %w", installErr, rollbackErr)
		_ = b.writeUpdateState(UpdateState{State: "failed", StartedAt: started, Info: &info, ErrorCode: "rollback_failed", Error: combined.Error()})
		return combined
	}
	_ = b.writeUpdateState(UpdateState{State: "rolled_back", StartedAt: started, Info: &info, ErrorCode: "update_verification_failed", Error: installErr.Error()})
	return fmt.Errorf("update failed and was rolled back: %w", installErr)
}

func (b Backend) startUpdateWorker() (map[string]any, error) {
	if b.Testing {
		return nil, errors.New("update worker is unavailable in an offline test root")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer null.Close()
	command := exec.Command(executable, "update_worker")
	command.Stdin = null
	command.Stdout = null
	command.Stderr = null
	if err := command.Start(); err != nil {
		return nil, err
	}
	pid := command.Process.Pid
	if err := command.Process.Release(); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "state": "accepted", "pid": pid}, nil
}

func (b Backend) autoUpdate(ctx context.Context) (map[string]any, error) {
	settings, err := LoadSettings(b.Root)
	if err != nil {
		return nil, err
	}
	if !settings.AutoUpdate {
		return map[string]any{"ok": true, "state": "disabled"}, nil
	}
	if err := b.runUpdate(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "state": "completed"}, nil
}
