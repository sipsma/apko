// Copyright 2022 Chainguard, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package build

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"
)

// Builds the image in Context.WorkDir.
func (bc *Context) BuildImage() error {
	log.Printf("doing pre-flight checks")
	err := bc.ImageConfiguration.Validate()
	if err != nil {
		return fmt.Errorf("failed to validate configuration: %w", err)
	}

	log.Printf("building image fileystem in %s", bc.WorkDir)

	// initialize apk
	err = bc.InitApkDB()
	if err != nil {
		return fmt.Errorf("failed to initialize apk database: %w", err)
	}

	var eg errgroup.Group

	eg.Go(func() error {
		err = bc.InitApkKeyring()
		if err != nil {
			return fmt.Errorf("failed to initialize apk keyring: %w", err)
		}
		return nil
	})

	eg.Go(func() error {
		err = bc.InitApkRepositories()
		if err != nil {
			return fmt.Errorf("failed to initialize apk repositories: %w", err)
		}
		return nil
	})

	eg.Go(func() error {
		err = bc.InitApkWorld()
		if err != nil {
			return fmt.Errorf("failed to initialize apk world: %w", err)
		}
		return nil
	})

	if err := eg.Wait(); err != nil {
		return err
	}

	// sync reality with desired apk world
	err = bc.FixateApkWorld()
	if err != nil {
		return fmt.Errorf("failed to fixate apk world: %w", err)
	}

	eg.Go(func() error {
		if err := bc.normalizeApkScriptsTar(); err != nil {
			return fmt.Errorf("failed to normalize scripts.tar: %w", err)
		}
		return nil
	})

	eg.Go(func() error {
		if err = bc.MutateAccounts(); err != nil {
			return fmt.Errorf("failed to mutate accounts: %w", err)
		}
		return nil
	})

	if err := eg.Wait(); err != nil {
		return err
	}

	// maybe install busybox symlinks
	if bc.UseProot {
		err = bc.InstallBusyboxSymlinks()
		if err != nil {
			return fmt.Errorf("failed to install busybox symlinks: %w", err)
		}
	}

	// write service supervision tree
	err = bc.WriteSupervisionTree()
	if err != nil {
		return fmt.Errorf("failed to write supervision tree: %w", err)
	}

	// generate SBOM
	if bc.SBOMPath != "" {
		err = bc.GenerateSBOM()
		if err != nil {
			return fmt.Errorf("failed to generate SBOM: %w", err)
		}
	}

	log.Printf("finished building filesystem in %s", bc.WorkDir)
	return nil
}

// Installs the BusyBox symlinks, if appropriate.
func (bc *Context) InstallBusyboxSymlinks() error {
	path := filepath.Join(bc.WorkDir, "bin", "busybox")

	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return err
	}

	// use proot + qemu to run the installer
	err = bc.ExecuteChroot("/bin/busybox", "--install", "-s")
	if err != nil {
		return fmt.Errorf("failed to install busybox symlinks: %w", err)
	}

	return nil
}
