package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/852hamza/chowki/internal/buildinfo"
	"github.com/852hamza/chowki/internal/config"
)

func readDeploy(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The container's configuration keeps the settings of the one that chowki
// init writes, with its files in the data volume, and listens on every
// address: inside a container, localhost would take no connection from
// outside it, and Compose publishes the port to this machine only.
func TestDeployConfig(t *testing.T) {
	none := func(string) (string, bool) { return "", false }
	image, err := config.Parse([]byte(readDeploy(t, "deploy/chowki.yaml")), none)
	if err != nil {
		t.Fatalf("deploy/chowki.yaml: %v", err)
	}
	starter, err := config.Parse(config.Starter(buildinfo.Project().DocsURL()), none)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{image.Storage.DSN: "file:/var/lib/chowki/",
		image.Security.MasterKeyFile: "/var/lib/chowki/"} {
		if !strings.HasPrefix(path, want) {
			t.Errorf("%s isn't in the data volume, /var/lib/chowki", path)
		}
	}
	if image.Server.Listen != ":8080" || starter.Server.Listen != "localhost:8080" {
		t.Errorf("the image listens on %q and chowki init on %q; want :8080 and localhost:8080",
			image.Server.Listen, starter.Server.Listen)
	}
	image.Storage.DSN, image.Security.MasterKeyFile = starter.Storage.DSN, starter.Security.MasterKeyFile
	image.Server.Listen = starter.Server.Listen
	if !reflect.DeepEqual(image, starter) {
		t.Errorf("deploy/chowki.yaml differs from the starter configuration:\n%+v\n%+v", image, starter)
	}
}

// The Dockerfile pins its base images, runs as a non-root user, and points
// CHOWKI_CONFIG at the configuration it copies; compose.yaml runs the
// published image, which the Release workflow tags latest, unless
// CHOWKI_IMAGE names another.
func TestDockerfile(t *testing.T) {
	dockerfile := readDeploy(t, "deploy/Dockerfile")
	froms := regexp.MustCompile(`(?m)^FROM\s+(?:--platform=\S+\s+)?(\S+)`).FindAllStringSubmatch(dockerfile, -1)
	if len(froms) == 0 {
		t.Fatal("no FROM lines")
	}
	pinned := regexp.MustCompile(`^[\w./-]+:[\w.-]+@sha256:[0-9a-f]{64}$`)
	for _, m := range froms {
		if !pinned.MatchString(m[1]) {
			t.Errorf("%s isn't pinned to a digest", m[1])
		}
	}
	if !strings.Contains(dockerfile, "\nUSER 65532:65532\n") {
		t.Error("the image doesn't run as the nonroot user")
	}
	// COPY --chmod gives its mode to the folders that it creates, which
	// locked the user out of /etc/chowki.
	if regexp.MustCompile(`(?m)^COPY\s.*--chmod`).MatchString(dockerfile) {
		t.Error("the Dockerfile uses COPY --chmod; set modes in the build stage")
	}
	if !strings.Contains(dockerfile, "deploy/chowki.yaml /out/etc/chowki/chowki.yaml") ||
		!strings.Contains(dockerfile, "ENV "+configEnv+"=/etc/chowki/chowki.yaml") {
		t.Errorf("%s doesn't name the configuration that the image holds", configEnv)
	}

	var compose struct {
		Services map[string]struct {
			Image string `yaml:"image"`
			Ports []string
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(readDeploy(t, "deploy/compose.yaml")), &compose); err != nil {
		t.Fatal(err)
	}
	svc := compose.Services["chowki"]
	published := buildinfo.Project().Image()
	if want := "${CHOWKI_IMAGE:-" + published + ":latest}"; svc.Image != want {
		t.Errorf("compose.yaml runs %q, want %q", svc.Image, want)
	}
	if !strings.Contains(readDeploy(t, ".github/workflows/release.yml"), `tags="$tags,$image:latest"`) {
		t.Error("the Release workflow no longer tags the image latest, which compose.yaml runs")
	}
	for _, p := range svc.Ports {
		if !strings.HasPrefix(p, "127.0.0.1:") {
			t.Errorf("compose.yaml publishes %s beyond this machine", p)
		}
	}
}
