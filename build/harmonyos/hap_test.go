package harmonyos

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"slices"
	"testing"
)

func TestPreviewArchive(t *testing.T) {
	path := os.Getenv("WAYGU_TEST_HAP")
	if path == "" {
		t.Skip("requires a freshly built Waygu HAP")
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	config, err := fs.ReadFile(archive, "module.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		App    struct{ BundleName string }
		Module struct{ DeviceTypes []string }
	}
	if err := json.Unmarshal(config, &manifest); err != nil {
		t.Fatal(err)
	}
	if bundle := manifest.App.BundleName; bundle != "dev.nsgo.wayguvideo" &&
		bundle != "dev.nsgo.wayguvideo.nsgohost000000000000000000000000000000000000" {
		t.Fatalf("unexpected bundle name %q", bundle)
	}
	for _, device := range []string{"default", "tablet"} {
		if !slices.Contains(manifest.Module.DeviceTypes, device) {
			t.Errorf("manifest is missing device type %s", device)
		}
	}

	// The SDK host and the independently compiled Wasm must agree on the
	// bridge. Checking both catches a missing module replacement in either build.
	for _, artifact := range []struct {
		path    string
		markers []string
	}{
		{"ets/modules.abc", []string{"secretKey", "nsgo-encrypted-secrets-key-v1"}},
		{"resources/rawfile/web/app.wasm", []string{"\x00asm", "secretKey", "encrypted-secrets"}},
	} {
		content, err := fs.ReadFile(archive, artifact.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range artifact.markers {
			if !bytes.Contains(content, []byte(marker)) {
				t.Errorf("%s is missing %q", artifact.path, marker)
			}
		}
	}
}
