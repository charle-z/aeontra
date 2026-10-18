//go:build !windows

package buildspike

import (
	"errors"
	"fmt"
)

const maxBuildkitConfigBytes = 16 << 10

func RenderBuildkitConfig(config Config) ([]byte, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	body := fmt.Sprintf(`root = %q

[grpc]
  address = [%q]

[worker.oci]
  enabled = true
  rootless = true
  noProcessSandbox = false
  gc = true
  max-parallelism = 1
  reservedSpace = "1GB"
  maxUsedSpace = "4GB"
  minFreeSpace = "8GB"

[worker.containerd]
  enabled = false
`, config.StateRoot, "unix://"+config.RuntimeRoot+"/buildkit/buildkitd.sock")
	if len(body) == 0 || len(body) > maxBuildkitConfigBytes {
		return nil, errors.New("buildspike: generated BuildKit configuration is invalid")
	}
	return []byte(body), nil
}
