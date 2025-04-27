package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/stdr"
	"golang.org/x/oauth2/google"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/config"
	"github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

// The following config is used to fetch a target from Jussi's GitHub repository example
const (
	metadataURL          = "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/metadata"
	targetsURL           = "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/targets"
	verbosity            = 4
	generateRandomFolder = false
)

var (
	serviceAccountKeyPath = "/home/sormazabal/artifact-downloader-key.json"
	jsonFilePath          = "/home/sormazabal/src/SALTO-client-linux/update_status.json"
	service               = "nebula-on-premise-linux"
	targetIndexFile       = "/home/sormazabal/src/SALTO-client-linux/data/nebula-on-premise-linux/nebula-on-premise-linux-index.json"
	newBinaryPath         = "/home/sormazabal/src/SALTO-client-linux/tmp/nebula-on-premise-linux.zip"
	destinationPath       = "/home/sormazabal/src/SALTO-client-linux/nebula-on-premise-linux.zip"
	SALTOLocation         = "/home/sormazabal/src/SALTO-client-linux"
	linkNameService       = "/usr/local/bin/nebula-on-premise-linux"
	linkNameConfig        = "/etc/nebula-on-premise-linux/nebula-on-premise-linux.yml"
)

// struct to store update status
type UpdateStatus struct {
	UpdateAvailable int `json:"update_available"`
	UpdateRequested int `json:"update_requested"`
}

// indexInfo is the structure in which the information from the general-service.json is stored.
type indexInfo struct {
	Bytes  string `json:"bytes"`
	Path   string `json:"path"`
	Hashes struct {
		Sha256 string `json:"sha256"`
	} `json:"hashes"`
	Version     string `json:"version"`
	ReleaseDate string `json:"release-date"`
}

// Main program
func main() {

	// First, a lof file will be opened in append mode, create if does not exist

	// Setting Logger's file location
	logFileLocation := filepath.Join(SALTOLocation, "nebula_tuf_client.log")

	logFile, err := os.OpenFile(logFileLocation, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close() // Ensure file is closed when program exits

	// Create a MultiWriter to log to both stdout and file
	multiWriter := io.MultiWriter(os.Stdout, logFile)

	// Create logger 2 for applying releases
	stdLogger2 := log.New(multiWriter, "ApplyReleaseImpl:", log.LstdFlags)

	// Set logger to use both stdout and file
	metadata.SetLogger(stdr.New(stdLogger2))

	// Retrieve and use logger
	ApplyReleaseImplLogger := metadata.GetLogger()

	// Create logger 1 for checking updates

	stdLogger1 := log.New(multiWriter, "CheckForUpdateImpl: ", log.LstdFlags)

	// Set logger to use both stdout and file
	metadata.SetLogger(stdr.New(stdLogger1))

	CheckForUpdateImplLogger := metadata.GetLogger()

	// Set verbosity level
	stdr.SetVerbosity(verbosity)

	// initialize environment - temporary folders, etc.
	metadataDir, err := InitEnvironment()
	if err != nil {
		CheckForUpdateImplLogger.Error(err, "Failed to initialize environment")
	}

	// initialize client with Trust-On-First-Use
	err = InitTrustOnFirstUse(metadataDir)
	if err != nil {
		CheckForUpdateImplLogger.Error(err, "Trust-On-First-Use failed")
	}

	// getting the current version
	currentVersion, err := readCurrentVersion()

	if err != nil {
		CheckForUpdateImplLogger.Error(err, "❌There has been an error while reading the current version❌")
	}
	msg := fmt.Sprintf("🟣Current Version is %s🟣", currentVersion)
	CheckForUpdateImplLogger.Info(msg)

	// getting the previous version folder
	previousVersion, err := getPreviousVersion(currentVersion)

	if err != nil {
		CheckForUpdateImplLogger.Error(err, "❌There has been an error while reading the previous version❌")
	}

	msg = fmt.Sprintf("🟣Previous Version is %s🟣", previousVersion)
	CheckForUpdateImplLogger.Info(msg)

	var wg sync.WaitGroup
	wg.Add(1)

	// Go routine 1 for setting the TUF updater
	go func() {
		defer wg.Done()

		// the updater needs to be looking for new updates every x time
		for {

			// downloading general-service-index.json
			_, foundDesiredTargetIndexLocally, err := DownloadTargetIndex(metadataDir, service)

			if err != nil {
				CheckForUpdateImplLogger.Error(err, "Download index file failed")
			}

			// if there is a new one, this will mean that is initializing for the first time or that there is a new update
			if foundDesiredTargetIndexLocally == 0 && err == nil {
				err := setUpdateStatus(1)
				if err != nil {
					CheckForUpdateImplLogger.Error(err, "❌ Error updating update_status.json")
				} else {
					CheckForUpdateImplLogger.Info("✅Successfully set update_status.json to update_available: 1✅")
				}

			} else {
				CheckForUpdateImplLogger.Info("The local index file is the most updated one")
			}

			time.Sleep(time.Second * 60)

		}
	}()
	//

	// Go routine 2 that is always looking if the user has requested the update
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {

			// every x time it will be reading if the user has requested a new update
			updateRequested, err := ReadUpdateRequested(jsonFilePath)

			if err != nil {
				ApplyReleaseImplLogger.Error(err, "There has been an error while reading the update requested Value")
			}

			// if the user has pushed the botton, the new server should be executed.
			if updateRequested == 1 {

				var data map[string]indexInfo
				msg = fmt.Sprintf("The index file is located in: %s ", targetIndexFile)
				ApplyReleaseImplLogger.Info(msg)

				// read the actual JSON file content
				fileContent, err := os.ReadFile(targetIndexFile)
				if err != nil {
					ApplyReleaseImplLogger.Error(err, "Fail to read the index file")
				}

				// parse JSON into the map
				err = json.Unmarshal(fileContent, &data)
				if err != nil {
					ApplyReleaseImplLogger.Error(err, "Error parsing JSON")
				}

				// getting service path
				servicePath := data[service].Path

				// download the artifact without specifying the file type
				err = downloadArtifact(serviceAccountKeyPath, servicePath, newBinaryPath, ApplyReleaseImplLogger)
				if err != nil {
					ApplyReleaseImplLogger.Error(err, "Failed to download binary")
					os.Exit(1)
				}

				// make sure the new binary is executable
				err = os.Chmod(newBinaryPath, 0755)
				if err != nil {
					ApplyReleaseImplLogger.Error(err, "Failed to set executable permissions")
				}

				// verifying that the downloaded file is integrate and authentic
				err = verifyingDownloadedFile(targetIndexFile, newBinaryPath, ApplyReleaseImplLogger)

				if err == nil {
					// Replace old binary
					err = os.Rename(newBinaryPath, destinationPath)
					if err != nil {
						ApplyReleaseImplLogger.Error(err, "Failed to rename the binary")
					}
				}

				serviceVersion := data[service].Version

				// unziping and setting the update status to 0
				unzipAndSetStatus(serviceVersion, ApplyReleaseImplLogger)

				targetFileService := filepath.Join(SALTOLocation, serviceVersion, "bin", service)
				targetFileConfig := filepath.Join(SALTOLocation, serviceVersion, "config", "nebula-on-premise-linux.yml")

				// 1) Updating symlink

				// symlink for service
				if err := updateSymlink(targetFileService, linkNameService); err != nil {
					ApplyReleaseImplLogger.Error(err, "Error updating symlink")
					return
				}
				ApplyReleaseImplLogger.Info("Symlink updated to point to:", targetFileService)

				// symlink for config
				if err := updateSymlink(targetFileConfig, linkNameConfig); err != nil {
					ApplyReleaseImplLogger.Error(err, "Error updating symlink")
					return
				}
				ApplyReleaseImplLogger.Info("Symlink updated to point to:", targetFileConfig)

				// 2) Reload and restart the service
				ctx := context.Background()
				if err := reloadAndRestartUnit(ctx, "nebula-on-premise-linux.service"); err != nil {
					ApplyReleaseImplLogger.Error(err, "Error restarting service")
					return
				}

				ApplyReleaseImplLogger.Info("Service reloaded and restarted successfully!")

				// Delete the previous version's folder

				msg = fmt.Sprintf("🟣The previous version is %s🟣", previousVersion)
				ApplyReleaseImplLogger.Info(msg)

				previousVersionPath := filepath.Join(SALTOLocation, previousVersion)
				err = os.RemoveAll(previousVersionPath)

				ApplyReleaseImplLogger.Info("🟠Deleting previous version folder🟠")
				if err != nil {
					ApplyReleaseImplLogger.Error(err, "Error deleting the previous folder")
				}

				// The previus version is what has been stored in current version
				previousVersion = currentVersion

				msg = fmt.Sprintf("🟣The previous version is %s🟣", previousVersion)
				ApplyReleaseImplLogger.Info(msg)

				currentVersion, err = readCurrentVersion()

				msg = fmt.Sprintf("🟣Current Version is %s🟣", currentVersion)
				ApplyReleaseImplLogger.Info(msg)

				if err != nil {
					ApplyReleaseImplLogger.Error(err, "Error reading the current version")
				}

			}
			time.Sleep(time.Second * 5)
		}
	}()
	//
	wg.Wait()
}

// InitEnvironment prepares the local environment for TUF- temporary folders, etc.
func InitEnvironment() (string, error) {
	var tmpDir string

	if !generateRandomFolder {
		tmpDir = filepath.Join(SALTOLocation, "tmp")
		// create a temporary folder for storing the demo artifacts
		os.Mkdir(tmpDir, 0750)
	} else {
		// create a temporary folder for storing the demo artifacts
		_, err := os.MkdirTemp(SALTOLocation, "tmp")
		if err != nil {
			return "", fmt.Errorf("failed to create a temporary folder: %w", err)
		}
	}

	// create a destination folder for storing the downloaded target
	os.Mkdir(filepath.Join(SALTOLocation, "data"), 0750)
	return tmpDir, nil
}

// InitTrustOnFirstUse initialize local trusted metadata (Trust-On-First-Use)
func InitTrustOnFirstUse(metadataDir string) error {
	// check if there's already a local root.json available for bootstrapping trust
	_, err := os.Stat(filepath.Join(metadataDir, "root.json"))
	if err == nil {
		return nil
	}

	// download the initial root metadata so we can bootstrap Trust-On-First-Use
	rootURL, err := url.JoinPath(metadataURL, "1.root.json")
	if err != nil {
		return fmt.Errorf("failed to create URL path for 1.root.json: %w", err)
	}

	req, err := http.NewRequest("GET", rootURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}

	client := http.DefaultClient

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to executed the http request: %w", err)
	}

	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read the http request body: %w", err)
	}

	// write the downloaded root metadata to file
	err = os.WriteFile(filepath.Join(metadataDir, "root.json"), data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write root.json metadata: %w", err)
	}
	return nil
}

// Reading the version of the current running server. For that, the general_service_index.json
// version will be downloaded.

func readCurrentVersion() (string, error) {

	var data map[string]indexInfo

	// Read the actual JSON file content
	fileContent, err := os.ReadFile(targetIndexFile)
	if err != nil {
		return "", fmt.Errorf("failed to read index file: %w", err)
	}

	// Parse JSON into the map
	err = json.Unmarshal(fileContent, &data)
	if err != nil {
		return "", fmt.Errorf("error parsin the JSON: %w", err)
	}

	currentVersion := data[service].Version

	return currentVersion, nil
}

// getPreviousVersion gets the previous running version of the service.
// This will first read the folders that have version naming structure and the previous version will
// be the one that is different from the currentVersion
func getPreviousVersion(currentVersion string) (string, error) {
	var previousVersion string

	// Regular expression to match versioned folders
	versionRegex := regexp.MustCompile(`^v\d{4}\.\d{2}\.\d{2}-sha\.[a-fA-F0-9]{7}$`)

	// Read the directory
	entries, err := os.ReadDir(SALTOLocation)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}

	var versions []string

	// Filter versioned folders
	for _, entry := range entries {
		if entry.IsDir() && versionRegex.MatchString(entry.Name()) {
			versions = append(versions, entry.Name())
		}
	}

	// Ensure we have exactly two versions
	if len(versions) != 2 {
		return "", fmt.Errorf("expected 2 versioned folders, found %d", len(versions))
	}

	// Identify the previous version (the one different from currentVersion)
	for _, version := range versions {
		if version != currentVersion {
			previousVersion = version
			break
		}
	}

	if previousVersion == "" {
		return "", fmt.Errorf("previous version not found")
	}

	return previousVersion, nil
}

// DownloadTargetIndex downloads the target file using Updater. The Updater refreshes the top-level metadata,
// get the target information, verifies if the target is already cached, and in case it
// is not cached, downloads the target file.
func DownloadTargetIndex(localMetadataDir, service string) ([]byte, int, error) {

	serviceFilePath := filepath.Join(service, fmt.Sprintf("%s-index.json", service))

	rootBytes, err := os.ReadFile(filepath.Join(localMetadataDir, "root.json"))
	if err != nil {
		return nil, 0, err
	}

	// create updater configuration
	cfg, err := config.New(metadataURL, rootBytes) // default config
	if err != nil {
		return nil, 0, err
	}

	cfg.LocalMetadataDir = localMetadataDir
	cfg.LocalTargetsDir = filepath.Join(SALTOLocation, "data")
	cfg.RemoteTargetsURL = targetsURL
	cfg.PrefixTargetsWithHash = true

	// create a new Updater instance
	up, err := updater.New(cfg)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create Updater instance: %w", err)
	}

	// try to build the top-level metadata
	err = up.Refresh()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to refresh trusted metadata: %w", err)
	}

	// Decode serviceFilePath before calling GetTargetInfo
	decodedServiceFilePath, _ := url.QueryUnescape(serviceFilePath)

	// Get metadata info
	ti, err := up.GetTargetInfo(decodedServiceFilePath)
	if err != nil {
		return nil, 0, fmt.Errorf("getting info for target index \"%s\": %w", serviceFilePath, err)
	}

	os.Mkdir(filepath.Join(SALTOLocation, "data", service), 0750)

	targetFilePath := filepath.Join(SALTOLocation, "data", service, fmt.Sprintf("%s-index.json", service))
	os.MkdirAll(filepath.Dir(targetFilePath), 0750) // Ensure the directory exists

	path, tb, err := up.FindCachedTarget(ti, targetFilePath)

	if err != nil {
		return nil, 0, fmt.Errorf("failed to find if there is a cachet target: %w", err)
	}

	if path != "" {
		// Cached version found
		fmt.Println("\U0001F34C CACHE HIT")
		return tb, 1, nil
	}

	// Ensure it is unescaped
	decodedTargetFilePath, _ := url.QueryUnescape(targetFilePath)

	// Now download
	targetfilePath, tb, err := up.DownloadTarget(ti, decodedTargetFilePath, "")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download target index file %s - %w", service, err)
	}

	fmt.Printf(" 🎯📄The target File Path is: %s 🎯📄", targetfilePath)

	return tb, 0, nil
}

// Function to update update_status.json
func setUpdateStatus(value int) error {
	// Create struct with new value
	updateStatus := UpdateStatus{UpdateAvailable: value}

	// Convert struct to JSON
	file, err := json.MarshalIndent(updateStatus, "", "  ")
	if err != nil {
		return err
	}

	// Write JSON to file
	err = os.WriteFile(jsonFilePath, file, 0644)
	if err != nil {
		return err
	}
	return nil
}

// ReadUpdateRequested extracts the "update_requested" value from a JSON file
func ReadUpdateRequested(jsonFilePath string) (int, error) {
	// Read the JSON file content
	fileContent, err := os.ReadFile(jsonFilePath)
	if err != nil {
		return 0, fmt.Errorf("failed to read JSON file: %v", err)
	}

	// Unmarshal JSON into struct
	var status UpdateStatus
	err = json.Unmarshal(fileContent, &status)
	if err != nil {
		return 0, fmt.Errorf("error parsing JSON: %v", err)
	}

	return status.UpdateRequested, nil
}

// Downloading the artifact indicated in general-service.json
func downloadArtifact(serviceAccountKeyPath, servicePath, newBinaryPath string, ApplyReleaseImplLogger metadata.Logger) error {
	// Authenticate using the service account key
	ctx := context.Background()
	creds, err := google.CredentialsFromJSON(ctx, readFile(serviceAccountKeyPath, ApplyReleaseImplLogger), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return fmt.Errorf("failed to load service account credentials: %w", err)
	}

	// Create HTTP client with the token
	client := &http.Client{}
	req, err := http.NewRequest("GET", servicePath, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add Authorization header with Bearer token
	token, err := creds.TokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to retrieve token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	// Perform the request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download artifact, status code: %d", resp.StatusCode)
	}

	// Determine the file name from the Content-Disposition header or use a default name
	contentDisposition := resp.Header.Get("Content-Disposition")
	fileName := newBinaryPath
	if contentDisposition != "" {
		_, params, err := mime.ParseMediaType(contentDisposition)
		if err == nil {
			if name, ok := params["filename"]; ok {
				fileName = name
			}
		}
	}
	ApplyReleaseImplLogger.Info("Saving file as: %s", fileName)

	// Write the response to a file
	out, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// readFile reads the content of the service account key JSON file.
func readFile(path string, ApplyReleaseImplLogger metadata.Logger) []byte {
	content, err := os.ReadFile(path)
	if err != nil {
		ApplyReleaseImplLogger.Error(err, "Error reading file")
		os.Exit(1)
	}
	return content
}

// verifyingDownloadedFile verifies a file.
func verifyingDownloadedFile(targetIndexFile, DonwloadedFilePath string, ApplyReleaseImplLogger metadata.Logger) error {

	var data map[string]indexInfo

	// Read the actual JSON file content
	fileContent, err := os.ReadFile(targetIndexFile)
	if err != nil {
		return fmt.Errorf("failed to read index file: %w", err)
	}

	// Parse JSON into the map
	err = json.Unmarshal(fileContent, &data)
	if err != nil {
		ApplyReleaseImplLogger.Error(err, "Error parsing JSON")
		return err
	}

	indexHash := data[service].Hashes.Sha256

	ApplyReleaseImplLogger.Info("The hash from the nebula-service-index.json is %s", indexHash)

	// Computing the hash of the downloaded file

	// Compute the SHA256 hash
	downloadedFilehash, err := ComputeSHA256(DonwloadedFilePath)

	ApplyReleaseImplLogger.Info("Downloaded file hash is: %s", downloadedFilehash)

	if err != nil {
		ApplyReleaseImplLogger.Error(err, "Error computing hash")
		return fmt.Errorf("error while computing the hash")
	}

	if indexHash == downloadedFilehash {
		ApplyReleaseImplLogger.Info("The target file has been downloaded successfully!")
	} else {
		return fmt.Errorf("there has been an error while downloading the file, the hashes do not match")
	}
	return nil
}

// Computing the SHA256 of a file.
func ComputeSHA256(filePath string) (string, error) {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create a SHA256 hash object
	hasher := sha256.New()

	// Copy the file contents into the hasher
	// This reads the file in chunks to handle large files efficiently
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to compute hash: %w", err)
	}

	// Get the final hash as a byte slice and convert to a hexadecimal string
	hash := hasher.Sum(nil)
	return fmt.Sprintf("%x", hash), nil
}

// Unzipping the downloaded target and setting the update status to 0.
func unzipAndSetStatus(serviceVersion string, ApplyReleaseImplLogger metadata.Logger) {

	destinationPathUnzip := ""
	destinationPathUnzip = fmt.Sprintf("%s/%s", SALTOLocation, serviceVersion)

	// Unzipping the downloaded target
	if err := Unzip(destinationPath, destinationPathUnzip); err != nil {
		ApplyReleaseImplLogger.Error(err, "❌Error unzipping new binary❌")
	} else {
		ApplyReleaseImplLogger.Info("✅Successfully unzipped the new binary✅")
	}

	// Removing what has been unzipped
	os.Remove(destinationPath)

	// Setting update status to 0
	setUpdateStatus(0)

}

// Unzipping a .zip and relocating it.
func Unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := r.Close(); err != nil {
			panic(err)
		}
	}()

	os.MkdirAll(dest, 0755)

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() {
			if err := rc.Close(); err != nil {
				panic(err)
			}
		}()

		path := filepath.Join(dest, f.Name)

		// Check for ZipSlip (Directory traversal)
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", path)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
		} else {
			os.MkdirAll(filepath.Dir(path), f.Mode())
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					panic(err)
				}
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range r.File {
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}
	return nil
}

// It reloads and restarts the unit
func reloadAndRestartUnit(ctx context.Context, unitName string) error {
	// Connect to systemd via D-Bus using the context-aware method
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer conn.Close()

	// Daemon-reload with context
	if err := conn.ReloadContext(ctx); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	// Restart the unit with context
	jobID, err := conn.RestartUnitContext(ctx, unitName, "replace", nil)
	if err != nil {
		return fmt.Errorf("failed to restart unit %s: %w", unitName, err)
	}

	fmt.Printf("Restart job queued: %v\n", jobID)
	return nil
}

// updateSymlink updates the symlink
func updateSymlink(newTarget, linkName string) error {
	if err := os.Remove(linkName); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old symlink: %w", err)
	}
	if err := os.Symlink(newTarget, linkName); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}
	return nil
}
