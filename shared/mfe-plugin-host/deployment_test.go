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
			compose := readRepositoryFile("demos", "01-dictionary", "deploy", "cell", "compose.dedicated.yaml")
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
			compose := readRepositoryFile("demos", "01-dictionary", "deploy", "cell", "compose.dedicated.yaml")
			for _, plugin := range migratedPlugins {
				service := composeService(compose, plugin+"-frontend")
				Expect(service).NotTo(ContainSubstring("- frontend"), plugin)
				Expect(service).To(ContainSubstring("- backend"), plugin)
				Expect(service).NotTo(ContainSubstring("proxy_pass"), plugin)
				Expect(service).NotTo(ContainSubstring("extra_hosts"), plugin)
				// One published port, still. ADR-055 made it a ${VAR:-default},
				// so the literal 711x moved into the default half.
				Expect(strings.Count(service, "- \"${PLUGIN_")).To(Equal(1), plugin)
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

	// The reset notice's grants are the rule made server-enforced (BR-AS73):
	// one party may state a catalogue loss and everyone else may only hear it.
	// A spec, not a comment in the bootstrap, because a grant that quietly
	// widens is exactly the kind of change nothing else reports.
	Context("BR-AS73 — who may state a catalogue reset, and who may only hear one", func() {
		const subject = "notify._platform.mfe-registry.entries.reset"

		grant := func(bootstrap, flag, user string) string {
			at := strings.Index(bootstrap, "--name "+user+" \\\n")
			Expect(at).To(BeNumerically(">=", 0), user)
			rest := bootstrap[at:]
			line := strings.Index(rest, flag+" ")
			Expect(line).To(BeNumerically(">=", 0), user+" "+flag)
			rest = rest[line+len(flag)+2:]
			return rest[:strings.Index(rest, "'")]
		}

		It("lets only the registry publish it, and only the announcing plugins hear it", func() {
			bootstrap := readRepositoryFile("demos", "01-dictionary", "nats", "bootstrap-operator.sh")
			Expect(grant(bootstrap, "--allow-pub", "mfe-registry-service")).To(ContainSubstring(subject))
			Expect(grant(bootstrap, "--allow-sub", "mfe-registry-service")).NotTo(ContainSubstring("entries.reset"))
			Expect(grant(bootstrap, "--allow-sub", `"$holder"`)).To(ContainSubstring(subject))
			Expect(grant(bootstrap, "--allow-pub", `"$holder"`)).NotTo(ContainSubstring("entries.reset"))
		})

		// demo-catalog has no announce grant, so hearing the notice would let
		// it do nothing. Its entry comes back from the operator's preload.
		It("does not grant it to the curated plugin, which has nothing to re-announce", func() {
			bootstrap := readRepositoryFile("demos", "01-dictionary", "nats", "bootstrap-operator.sh")
			Expect(grant(bootstrap, "--allow-sub", "demo-catalog")).NotTo(ContainSubstring("entries.reset"))
		})

		It("is granted as one exact subject, never a prefix", func() {
			bootstrap := readRepositoryFile("demos", "01-dictionary", "nats", "bootstrap-operator.sh")
			for _, wildcard := range []string{"notify._platform.mfe-registry.entries.>", "notify._platform.mfe-registry.>"} {
				Expect(bootstrap).NotTo(ContainSubstring(wildcard), wildcard)
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
			compose := readRepositoryFile("demos", "01-dictionary", "deploy", "cell", "compose.dedicated.yaml")
			actual := composeService(compose, "example-plugin-frontend")
			// Order matters: the port VARIABLE is normalized before the id,
			// because PLUGIN_EXAMPLE_PORT does not contain "example-plugin"
			// but does contain the digits 7111 in its default.
			actual = strings.ReplaceAll(actual, "PLUGIN_EXAMPLE_PORT", "__PLUGIN_PORT_VAR__")
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
			for _, dir := range []string{
				"lab-shell/plugins",
				"demos/01-dictionary/nats",
				"demos/01-dictionary/deploy/cell",
				"demos/01-dictionary/deploy/environments",
				"scripts/templates",
			} {
				Expect(os.MkdirAll(filepath.Join(root, dir), 0o700)).To(Succeed())
			}
			copyTree(filepath.Join(repositoryRoot(), "lab-shell/plugins/example-plugin"), filepath.Join(root, "lab-shell/plugins/example-plugin"))
			// ADR-055 split the one flat file into bands, so a scaffolded
			// plugin now touches five files: the dedicated band for the
			// service and its volume, the runtime band for the registry's
			// allowlist and fetch map, and one env file per cell for the port.
			for _, file := range []string{
				"demos/01-dictionary/deploy/cell/compose.dedicated.yaml",
				"demos/01-dictionary/deploy/cell/compose.runtime.yaml",
				"demos/01-dictionary/deploy/environments/local-za-1.env",
				"demos/01-dictionary/deploy/environments/local-au-1.env",
				"demos/01-dictionary/nats/bootstrap-operator.sh",
				"demos/01-dictionary/README.md",
				"scripts/templates/plugin-compose.yml.tpl",
			} {
				copyFile(filepath.Join(repositoryRoot(), file), filepath.Join(root, file))
			}

			command := exec.Command(filepath.Join(repositoryRoot(), "scripts/new-plugin.sh"), "acme-widget", "7116")
			command.Env = append(os.Environ(), "PLUGIN_SCAFFOLD_ROOT="+root)
			output, err := command.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))

			compose := readFile(filepath.Join(root, "demos/01-dictionary/deploy/cell/compose.dedicated.yaml"))
			generated := strings.Join([]string{
				readFile(filepath.Join(root, "lab-shell/plugins/acme-widget/Dockerfile")),
				readFile(filepath.Join(root, "lab-shell/plugins/acme-widget/public/manifest.json")),
				composeService(compose, "acme-widget-frontend"),
			}, "\n---\n")
			golden := readRepositoryFile("scripts", "testdata", "new-plugin.golden")
			Expect(generated).To(Equal(strings.TrimSuffix(golden, "\n")))

			Expect(readFile(filepath.Join(root, "demos/01-dictionary/nats/bootstrap-operator.sh"))).To(ContainSubstring("  acme-widget\n"))
			Expect(compose).To(ContainSubstring("http://localhost:${PLUGIN_ACME_WIDGET_PORT:-7116}"))
			Expect(compose).To(ContainSubstring("  acme-widget-release:\n"))
			Expect(readFile(filepath.Join(root, "demos/01-dictionary/README.md"))).To(ContainSubstring("| Acme Widget plugin | http://localhost:7116 |"))

			// Both halves of the registry wiring, in the runtime band. An
			// allowlist without a fetch map, or either without the other, is
			// silent: the plugin comes up and its entries are refused.
			runtime := readFile(filepath.Join(root, "demos/01-dictionary/deploy/cell/compose.runtime.yaml"))
			Expect(runtime).To(ContainSubstring(`REGISTRY_ALLOWED_ORIGINS: "http://localhost:${PLUGIN_ACME_WIDGET_PORT:-7116},`))
			Expect(runtime).To(ContainSubstring(`"http://localhost:${PLUGIN_ACME_WIDGET_PORT:-7116}":"http://acme-widget-frontend:8080"`))

			// One port variable per cell, and au-1 is offset by +50. A plugin
			// that exists in one cell and not the other is the failure ADR-055
			// exists to prevent.
			Expect(readFile(filepath.Join(root, "demos/01-dictionary/deploy/environments/local-za-1.env"))).To(ContainSubstring("PLUGIN_ACME_WIDGET_PORT=7116\n"))
			Expect(readFile(filepath.Join(root, "demos/01-dictionary/deploy/environments/local-au-1.env"))).To(ContainSubstring("PLUGIN_ACME_WIDGET_PORT=7166\n"))
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
