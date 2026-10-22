package addonbuild

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AddonBackupMetaName is the sidecar name Place writes next to a backed-up
// VL.AddOn.dll. It records the OLD AddOn's proof at the moment it was moved out
// of bin/Custom, so a later restore can refuse an unverified backup (m343).
const AddonBackupMetaName = "addon-backup.meta"

// AddonBackupMeta is the sidecar content: the backed-up DLL's sha256 and its
// embedded build id, both read at backup time.
type AddonBackupMeta struct {
	Sha256  string `json:"sha256"`
	BuildID string `json:"build_id"`
}

// WriteAddonBackupMeta records the sha256 + embedded build id of the backed-up
// DLL at dllPath into <backupDir>/addon-backup.meta.
func WriteAddonBackupMeta(backupDir, dllPath string) error {
	sum, err := sha256File(dllPath)
	if err != nil {
		return err
	}
	id, err := BuildIDFromDLL(dllPath)
	if err != nil {
		return err
	}
	b, err := json.Marshal(AddonBackupMeta{Sha256: sum, BuildID: id})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(backupDir, AddonBackupMetaName), b, 0o644)
}

// ReadAddonBackupMeta reads <backupDir>/addon-backup.meta.
func ReadAddonBackupMeta(backupDir string) (AddonBackupMeta, error) {
	b, err := os.ReadFile(filepath.Join(backupDir, AddonBackupMetaName))
	if err != nil {
		return AddonBackupMeta{}, err
	}
	var m AddonBackupMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return AddonBackupMeta{}, err
	}
	return m, nil
}
