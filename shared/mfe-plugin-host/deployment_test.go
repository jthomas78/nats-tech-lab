package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var migratedPlugins = []string{
	"example-plugin",
	"example-plugin-slow",
	"example-plugin-activate-throws",
	"example-plugin-incompatible",
}

func repositoryRoot() string {
	_, file, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue())
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func readRepositoryFile(path ...string) string {
	raw, err := os.ReadFile(filepath.Join(append([]string{repositoryRoot()}, path...)...))
	Expect(err).NotTo(HaveOccurred())
	return string(raw)
}

func composeService(compose, name string) string {
	marker := "  " + name + ":\n"
	start := strings.Index(compose, marker)
	Expect(start).To(BeNumerically(">=", 0), "missing service %s", name)
	rest := compose[start+len(marker):]
	lines := strings.Split(rest, "\n")
	end := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
			end = i
			break
		}
	}
	return strings.Join(lines[:end], "\n")
}

var _ = Describe("Migrated plugin deployment", func() {
	Context("BR-AS15 — every plugin retains its own toolchain and image", func() {
		It("builds each plugin from its own package files and copies only dist into the shared base", func() {
			for _, plugin := range migratedPlugins {
				dockerfile := readRepositoryFile("lab-shell", "plugins", plugin, "Dockerfile")
				Expect(dockerfile).To(ContainSubstring(plugin+"/package.json"), plugin)
				Expect(dockerfile).To(ContainSubstring(plugin+"/package-lock.json"), plugin)
				Expect(dockerfile).To(ContainSubstring("RUN npm ci && npm run build"), plugin)
				Expect(dockerfile).To(ContainSubstring("FROM mfe-plugin-host"), plugin)
				final := dockerfile[strings.LastIndex(dockerfile, "FROM mfe-plugin-host"):]
				copyLines := []string{}
				for _, line := range strings.Split(final, "\n") {
					if strings.HasPrefix(line, "COPY ") {
						copyLines = append(copyLines, line)
					}
				}
				Expect(copyLines).To(Equal([]string{"COPY --from=build /repo/lab-shell/plugins/" + plugin + "/dist /srv"}), plugin)
			}
		})

		It("keeps exactly one service per announced plugin except the announcer-only 404 fixture", func() {
			compose := readRepositoryFile("demos", "01-dictionary", "docker-compose.yml")
			announcers := []string{}
			for _, line := range strings.Split(compose, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasSuffix(trimmed, "-announcer:") {
					announcers = append(announcers, trimmed)
				}
			}
			Expect(announcers).To(Equal([]string{"example-plugin-unreachable-announcer:"}))
		})

		// One network, not two, since Phase 15c. The second one existed so the
		// registry could dial a plugin's /healthz from inside the network, and
		// nothing dials one now — the plugin reports itself. The browser was
		// never on a docker network to begin with: it uses the published host
		// port, which the last assertion here still pins to exactly one.
		It("joins migrated plugins to the backend network only, without proxy, extra host, or extra port", func() {
			compose := readRepositoryFile("demos", "01-dictionary", "docker-compose.yml")
			for _, plugin := range migratedPlugins {
				service := composeService(compose, plugin+"-frontend")
				Expect(service).NotTo(ContainSubstring("- frontend"), plugin)
				Expect(service).To(ContainSubstring("- backend"), plugin)
				Expect(service).NotTo(ContainSubstring("proxy_pass"), plugin)
				Expect(service).NotTo(ContainSubstring("extra_hosts"), plugin)
				Expect(strings.Count(service, "- \"711")).To(Equal(1), plugin)
			}
		})
	})

	Context("BR-AS71 — fixture images carry no deployment origin", func() {
		It("keeps all five announced fixture manifests path-only", func() {
			for _, plugin := range append(append([]string{}, migratedPlugins...), "example-plugin-unreachable") {
				var manifest struct {
					Remote struct {
						URL string `json:"url"`
					} `json:"remote"`
				}
				Expect(json.Unmarshal([]byte(readRepositoryFile("lab-shell", "plugins", plugin, "public", "manifest.json")), &manifest)).To(Succeed())
				Expect(manifest.Remote.URL).To(HavePrefix("/"), plugin)
				Expect(manifest.Remote.URL).NotTo(HavePrefix("//"), plugin)
				Expect(manifest.Remote.URL).NotTo(ContainSubstring("://"), plugin)
			}
		})
	})

	Context("BR-AS67 — CLI and host use the same release implementation", func() {
		It("calls announcer.Start from both process entry points", func() {
			cli := readRepositoryFile("demos", "01-dictionary", "backend", "mfe-registry-service", "cmd", "announce-plugin", "main.go")
			host := readRepositoryFile("shared", "mfe-plugin-host", "main.go")
			Expect(cli).To(ContainSubstring("announcer.Start(ctx, cfg)"))
			Expect(host).To(ContainSubstring("announcer.Start"))
		})
	})
})

var _ = Describe("Plugin scaffolder", func() {
	Context("decision 9 — generated plugins match the migrated production shape", func() {
		It("keeps the Compose template synchronized with the real example service", func() {
			compose := readRepositoryFile("demos", "01-dictionary", "docker-compose.yml")
			actual := composeService(compose, "example-plugin-frontend")
			actual = strings.ReplaceAll(actual, "example-plugin", "__PLUGIN_ID__")
			actual = strings.ReplaceAll(actual, "7111", "__PLUGIN_PORT__")
			anchor := "    depends_on: &plugin_dependencies\n"
			anchorIndex := strings.Index(actual, anchor)
			Expect(anchorIndex).To(BeNumerically(">=", 0))
			actual = actual[:anchorIndex] + "    depends_on: *plugin_dependencies"

			template := readRepositoryFile("scripts", "templates", "plugin-compose.yml.tpl")
			expected := composeService(template, "__PLUGIN_ID__-frontend")
			Expect(strings.TrimSpace(actual)).To(Equal(strings.TrimSpace(expected)))
		})

		It("matches the golden fixture derived from example-plugin", func() {
			root := GinkgoT().TempDir()
			for _, dir := range []string{"lab-shell/plugins", "demos/01-dictionary/nats", "scripts/templates"} {
				Expect(os.MkdirAll(filepath.Join(root, dir), 0o700)).To(Succeed())
			}
			copyTree(filepath.Join(repositoryRoot(), "lab-shell/plugins/example-plugin"), filepath.Join(root, "lab-shell/plugins/example-plugin"))
			for _, file := range []string{"demos/01-dictionary/docker-compose.yml", "demos/01-dictionary/nats/bootstrap-operator.sh", "demos/01-dictionary/README.md", "scripts/templates/plugin-compose.yml.tpl"} {
				copyFile(filepath.Join(repositoryRoot(), file), filepath.Join(root, file))
			}

			command := exec.Command(filepath.Join(repositoryRoot(), "scripts/new-plugin.sh"), "acme-widget", "7116")
			command.Env = append(os.Environ(), "PLUGIN_SCAFFOLD_ROOT="+root)
			output, err := command.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))

			compose := readFile(filepath.Join(root, "demos/01-dictionary/docker-compose.yml"))
			generated := strings.Join([]string{
				readFile(filepath.Join(root, "lab-shell/plugins/acme-widget/Dockerfile")),
				readFile(filepath.Join(root, "lab-shell/plugins/acme-widget/public/manifest.json")),
				composeService(compose, "acme-widget-frontend"),
			}, "\n---\n")
			golden := readRepositoryFile("scripts", "testdata", "new-plugin.golden")
			Expect(generated).To(Equal(strings.TrimSuffix(golden, "\n")))

			Expect(readFile(filepath.Join(root, "demos/01-dictionary/nats/bootstrap-operator.sh"))).To(ContainSubstring("  acme-widget\n"))
			Expect(compose).To(ContainSubstring("http://localhost:7116"))
			Expect(compose).To(ContainSubstring(`"acme-widget":[]`))
			Expect(readFile(filepath.Join(root, "demos/01-dictionary/README.md"))).To(ContainSubstring("| Acme Widget plugin | http://localhost:7116 |"))
		})
	})
})

func copyTree(source, target string) {
	Expect(filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if info.IsDir() {
			if path != source && (info.Name() == "node_modules" || info.Name() == "dist") {
				return filepath.SkipDir
			}
			return os.MkdirAll(destination, info.Mode())
		}
		copyFile(path, destination)
		return nil
	})).To(Succeed())
}

func copyFile(source, target string) {
	Expect(os.MkdirAll(filepath.Dir(target), 0o700)).To(Succeed())
	raw, err := os.ReadFile(source)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(target, raw, 0o600)).To(Succeed())
}

func readFile(path string) string {
	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return string(raw)
}
