// Copyright Istio Authors
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

package components

import (
	"fmt"
	"os"
	"path"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/mesh"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/istio/pkg/config/event"
	kubelib "istio.io/istio/pkg/kube"
)

func TestProcessing_StartErrors(t *testing.T) {
	g := NewWithT(t)
	defer resetPatchTable()

loop:
	for i := 0; ; i++ {
		resetPatchTable()
		newInterfaces = func(string) (kube.Interfaces, error) {
			return kube.NewInterfacesFromClient(kubelib.NewFakeClient()), nil
		}

		e := fmt.Errorf("err%d", i)

		tmpDir, err := os.MkdirTemp(os.TempDir(), t.Name())
		g.Expect(err).To(BeNil())

		meshCfgDir := path.Join(tmpDir, "meshcfg")
		err = os.Mkdir(meshCfgDir, os.ModePerm)
		g.Expect(err).To(BeNil())

		meshCfgFile := path.Join(tmpDir, "meshcfg.yaml")
		_, err = os.Create(meshCfgFile)
		g.Expect(err).To(BeNil())

		args := settings.DefaultArgs()
		args.MeshConfigFile = meshCfgFile

		switch i {
		case 0:
			newInterfaces = func(string) (kube.Interfaces, error) {
				return nil, e
			}
		case 1:
			meshcfgNewFS = func(path string) (event.Source, error) { return nil, e }
		case 2:
			processorInitialize = func(processor.Settings) (*processing.Runtime, error) {
				return nil, e
			}
		default:
			break loop

		}

		p := NewProcessing(args)
		err = p.Start()
		g.Expect(err).NotTo(BeNil())
		t.Logf("%d) err: %v", i, err)
		p.Stop()
	}
}

func TestProcessing_Basic(t *testing.T) {
	g := NewWithT(t)
	resetPatchTable()
	defer resetPatchTable()

	newInterfaces = func(string) (kube.Interfaces, error) {
		return kube.NewInterfacesFromClient(kubelib.NewFakeClient()), nil
	}
	meshcfgNewFS = func(path string) (event.Source, error) { return mesh.NewInmemoryMeshCfg(), nil }

	args := settings.DefaultArgs()

	p := NewProcessing(args)
	err := p.Start()
	g.Expect(err).To(BeNil())

	p.Stop()
}
