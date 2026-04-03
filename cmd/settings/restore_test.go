package settings

import (
	"testing"

	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stretchr/testify/assert"
)

func TestBuildVolumeMounts_StackpacksLocalFileURI(t *testing.T) {
	tests := []struct {
		name              string
		stackpacks        *config.StackpacksConfig
		expectStackpacks  bool
		expectedMountPath string
	}{
		{
			name:             "no stackpacks config",
			stackpacks:       nil,
			expectStackpacks: false,
		},
		{
			name:             "stackpacks with no PVC",
			stackpacks:       &config.StackpacksConfig{LocalStackPacksURI: "file:///var/stackpacks_local"},
			expectStackpacks: false,
		},
		{
			name:              "stackpacks with file:// URI and PVC",
			stackpacks:        &config.StackpacksConfig{LocalStackPacksURI: "file:///var/stackpacks_local", PVC: "stackpacks-pvc"},
			expectStackpacks:  true,
			expectedMountPath: "/var/stackpacks_local",
		},
		{
			name:             "stackpacks with non-file URI and PVC",
			stackpacks:       &config.StackpacksConfig{LocalStackPacksURI: "s3://my-bucket/stackpacks", PVC: "stackpacks-pvc"},
			expectStackpacks: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Ensure the package-level fromPVC flag does not interfere
			fromPVC = false

			cfg := &config.Config{Stackpacks: tt.stackpacks}
			mounts := buildVolumeMounts(cfg)

			var stackpacksMount *struct{ Name, MountPath string }
			for _, m := range mounts {
				if m.Name == "stackpacks-local" {
					stackpacksMount = &struct{ Name, MountPath string }{m.Name, m.MountPath}
					break
				}
			}

			if tt.expectStackpacks {
				assert.NotNil(t, stackpacksMount, "expected stackpacks-local volume mount to be present")
				assert.Equal(t, tt.expectedMountPath, stackpacksMount.MountPath)
			} else {
				assert.Nil(t, stackpacksMount, "expected stackpacks-local volume mount to be absent")
			}
		})
	}
}
