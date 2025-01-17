package helm3

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"get.porter.sh/porter/pkg/exec/builder"
	"github.com/Masterminds/semver"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

// clientVersionConstraint represents the semver constraint for the Helm client version
// Currently, this mixin only supports Helm clients versioned v3.x.x
const clientVersionConstraint string = "^v3.x"

// BuildInput represents stdin passed to the mixin for the build command.
type BuildInput struct {
	Config MixinConfig
}

// MixinConfig represents configuration that can be set on the helm3 mixin in porter.yaml
// mixins:
// - helm3:
// 	  clientVersion: v3.8.2
// 	  clientPlatfrom: linux
// 	  clientArchitecture: amd64 | arm64 | arm | i386
//	  repositories:
//	    stable:
//		  url: "https://charts.helm.sh/stable"

type MixinConfig struct {
	ClientVersion      string `yaml:"clientVersion,omitempty"`
	ClientPlatfrom     string `yaml:"clientPlatfrom,omitempty"`
	ClientArchitecture string `yaml:"clientArchitecture,omitempty"`
	Repositories       map[string]Repository
}

type Repository struct {
	URL string `yaml:"url,omitempty"`
}

// Build will generate the necessary Dockerfile lines
// for an invocation image using this mixin
func (m *Mixin) Build(ctx context.Context) error {

	// Create new Builder.
	var input BuildInput
	err := builder.LoadAction(ctx, m.RuntimeConfig, "", func(contents []byte) (interface{}, error) {
		err := yaml.Unmarshal(contents, &input)
		return &input, err
	})
	if err != nil {
		return err
	}

	suppliedClientVersion := input.Config.ClientVersion
	if suppliedClientVersion != "" {
		ok, err := validate(suppliedClientVersion, clientVersionConstraint)
		if err != nil {
			return err
		}
		if !ok {
			return errors.Errorf("supplied clientVersion %q does not meet semver constraint %q",
				suppliedClientVersion, clientVersionConstraint)
		}
		m.HelmClientVersion = suppliedClientVersion
	}

	if input.Config.ClientPlatfrom != "" {
		m.HelmClientPlatfrom = input.Config.ClientPlatfrom
	}

	if input.Config.ClientArchitecture != "" {
		m.HelmClientArchitecture = input.Config.ClientArchitecture
	}
	// Install helm3
	fmt.Fprint(m.Out, "ENV HELM_EXPERIMENTAL_OCI=1")
	fmt.Fprintf(m.Out, "\nRUN apt-get update && apt-get install -y curl")
	fmt.Fprintf(m.Out, "\nRUN curl https://get.helm.sh/helm-%s-%s-%s.tar.gz --output helm3.tar.gz",
		m.HelmClientVersion, m.HelmClientPlatfrom, m.HelmClientArchitecture)
	fmt.Fprintf(m.Out, "\nRUN tar -xvf helm3.tar.gz && rm helm3.tar.gz")
	fmt.Fprintf(m.Out, "\nRUN mv linux-amd64/helm /usr/local/bin/helm3")
	fmt.Fprintf(m.Out, "\nRUN curl -o kubectl https://storage.googleapis.com/kubernetes-release/release/v1.22.1/bin/linux/amd64/kubectl &&\\")
	fmt.Fprintf(m.Out, "\n    mv kubectl /usr/local/bin && chmod a+x /usr/local/bin/kubectl\n")
	if len(input.Config.Repositories) > 0 {
		// Switch to a non-root user so helm is configured for the user the container will execute as
		fmt.Fprintln(m.Out, "USER ${BUNDLE_USER}")

		// Go through repositories
		names := make([]string, 0, len(input.Config.Repositories))
		for name := range input.Config.Repositories {
			names = append(names, name)
		}
		sort.Strings(names) //sort by key
		for _, name := range names {
			url := input.Config.Repositories[name].URL
			repositoryCommand, err := getRepositoryCommand(name, url)
			if err != nil {
				if m.DebugMode {
					fmt.Fprintf(m.Err, "DEBUG: addition of repository failed: %s\n", err.Error())
				}
			} else {
				fmt.Fprintln(m.Out, strings.Join(repositoryCommand, " "))
			}
		}
		// Make sure we update  the helm repositories
		// So we don\'t have to do it later
		fmt.Fprintln(m.Out, "RUN helm3 repo update")

		// Switch back to root so that subsequent mixins can install things
		fmt.Fprintln(m.Out, "USER root")
	}

	return nil
}

func getRepositoryCommand(name, url string) (repositoryCommand []string, err error) {

	var commandBuilder []string

	if url == "" {
		return commandBuilder, fmt.Errorf("repository url must be supplied")
	}

	commandBuilder = append(commandBuilder, "RUN", "helm3", "repo", "add", name, url)

	return commandBuilder, nil
}

// validate validates that the supplied clientVersion meets the supplied semver constraint
func validate(clientVersion, constraint string) (bool, error) {
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return false, errors.Wrapf(err, "unable to parse version constraint %q", constraint)
	}

	v, err := semver.NewVersion(clientVersion)
	if err != nil {
		return false, errors.Wrapf(err, "supplied client version %q cannot be parsed as semver", clientVersion)
	}

	return c.Check(v), nil
}
