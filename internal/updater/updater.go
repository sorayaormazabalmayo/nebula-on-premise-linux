package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	stdlog "log"

	"github.com/go-logr/stdr"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/config"
	"github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

const (
	metadataURL          = "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/metadata"
	targetsURL           = "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/targets"
	verbosity            = 0
	generateRandomFolder = false
)

var (
	services   = []string{"general-service"}
	jsonPath   = "update_status.json"
	checkDelay = 60 * time.Second
)

type UpdateStatus struct {
	UpdateAvailable int `json:"update_available"`
}

// Run executes the updater logic.
func Run() error {
	// Set up logging.
	metadata.SetLogger(stdr.New(stdlog.New(os.Stdout, "updater: ", stdlog.LstdFlags)))
	stdr.SetVerbosity(verbosity)
	log := metadata.GetLogger()

	metadataDir, err := InitEnvironment()
	if err != nil {
		log.Error(err, "Failed to initialize environment")
		return err
	}

	if err = InitTrustOnFirstUse(metadataDir); err != nil {
		log.Error(err, "Trust-On-First-Use failed")
		return err
	}

	// Check for updates in a loop.
	for {
		for _, service := range services {
			_, found, err := DownloadTargetIndex(metadataDir, service)
			if err != nil {
				log.Error(err, "Failed to download target index")
			}
			if found == 0 {
				if err := setUpdateStatus(1); err != nil {
					fmt.Println("Error updating update_status.json:", err)
				} else {
					fmt.Println("Update available flag set in update_status.json")
				}
			} else {
				fmt.Println("Local index is up-to-date.")
			}
		}
		time.Sleep(checkDelay)
	}
}

// InitEnvironment, InitTrustOnFirstUse, DownloadTargetIndex and setUpdateStatus
// are similar to your original updater functions. You can place them here or split them into
// multiple files if desired.

func InitEnvironment() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	var tmpDir string
	if !generateRandomFolder {
		tmpDir = filepath.Join(cwd, "tmp")
		os.Mkdir(tmpDir, 0750)
	} else {
		tmpDir, err = os.MkdirTemp(cwd, "tmp")
		if err != nil {
			return "", fmt.Errorf("failed to create a temporary folder: %w", err)
		}
	}
	os.Mkdir(filepath.Join(cwd, "data"), 0750)
	return tmpDir, nil
}

func InitTrustOnFirstUse(metadataDir string) error {
	rootPath := filepath.Join(metadataDir, "root.json")
	if _, err := os.Stat(rootPath); err == nil {
		return nil
	}
	rootURL, err := url.JoinPath(metadataURL, "1.root.json")
	if err != nil {
		return fmt.Errorf("failed to create URL for 1.root.json: %w", err)
	}
	resp, err := http.Get(rootURL)
	if err != nil {
		return fmt.Errorf("failed to GET 1.root.json: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read 1.root.json body: %w", err)
	}
	return os.WriteFile(rootPath, data, 0644)
}

func DownloadTargetIndex(metadataDir, service string) ([]byte, int, error) {
	serviceFilePath := filepath.Join(service, fmt.Sprintf("%s-index.json", service))
	fmt.Printf("DEBUG: serviceFilePath: %s\n", serviceFilePath)
	rootBytes, err := os.ReadFile(filepath.Join(metadataDir, "root.json"))
	if err != nil {
		return nil, 0, err
	}
	cfg, err := config.New(metadataURL, rootBytes)
	if err != nil {
		return nil, 0, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, 0, err
	}
	cfg.LocalMetadataDir = metadataDir
	cfg.LocalTargetsDir = filepath.Join(cwd, "data")
	cfg.RemoteTargetsURL = targetsURL
	cfg.PrefixTargetsWithHash = true

	up, err := updater.New(cfg)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create updater: %w", err)
	}
	if err = up.Refresh(); err != nil {
		return nil, 0, fmt.Errorf("failed to refresh metadata: %w", err)
	}
	ti, err := up.GetTargetInfo(serviceFilePath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get target info: %w", err)
	}
	os.Mkdir(filepath.Join(cwd, "data", service), 0750)
	targetPath := filepath.Join(cwd, "data", service, fmt.Sprintf("%s-index.json", service))
	os.MkdirAll(filepath.Dir(targetPath), 0750)
	path, tb, err := up.FindCachedTarget(ti, targetPath)
	fmt.Printf("DEBUG: Cached target file: %s\n", path)
	if err != nil {
		return nil, 0, fmt.Errorf("error checking cache: %w", err)
	}
	if path != "" {
		fmt.Println("CACHE HIT")
		return tb, 1, nil
	}
	_, tb, err = up.DownloadTarget(ti, targetPath, "")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download target index: %w", err)
	}
	return tb, 0, nil
}

func setUpdateStatus(value int) error {
	status := UpdateStatus{UpdateAvailable: value}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(jsonPath, data, 0644)
}
