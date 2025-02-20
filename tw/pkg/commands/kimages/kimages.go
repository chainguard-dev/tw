package kimages

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/chainguard-dev/clog"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
)

type cfg struct {
	AllNamespaces       bool
	Namespace           string
	Timeout             time.Duration
	EnforceRegistry     string
	EnforceRegistrySkip string
}

func Command() *cobra.Command {
	cfg := &cfg{}

	cmd := &cobra.Command{
		Use:          "kimages",
		Short:        "List images in Kubernetes resources",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd, args)
		},
	}

	cmd.Flags().StringVarP(&cfg.Namespace, "namespace", "n", "default", "namespace to install the release into")
	cmd.Flags().BoolVarP(&cfg.AllNamespaces, "all", "A", false, "search across all namespaces")
	cmd.Flags().DurationVarP(&cfg.Timeout, "timeout", "t", time.Minute, "timeout for the operation")
	cmd.Flags().StringVar(&cfg.EnforceRegistry, "enforce-registry", "", "enforce all discovered images belong to this registry")
	cmd.Flags().StringVar(&cfg.EnforceRegistrySkip, "enforce-registry-skip", "^$", "regex pattern to match on image references to skip enforcement of")

	return cmd
}

func (c *cfg) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	getter := genericclioptions.NewConfigFlags(false)

	f := util.NewFactory(getter)

	builder := f.NewBuilder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		DefaultNamespace()

	if c.AllNamespaces {
		builder.AllNamespaces(true)
	} else {
		builder.NamespaceParam(c.Namespace)
	}

	images := []ParsedImage{}
	if len(args) == 0 {
		// visit all resources with containers (pods)
		r := builder.ResourceTypes("pods").
			SelectAllParam(true).
			Flatten().Do()
		if err := r.Err(); err != nil {
			return fmt.Errorf("failed to build resource: %v", err)
		}

		if err := r.Visit(func(i *resource.Info, err error) error {
			if err != nil {
				return err
			}

			clog.InfoContext(ctx, "visit", "name", i.Name, "namespace", i.Namespace, "obj", i.String())

			// cast it to a pod
			pod, ok := i.Object.(*corev1.Pod)
			if !ok {
				return fmt.Errorf("failed to cast object to pod")
			}

			for _, c := range pod.Spec.Containers {
				pimage, err := parseImage(c.Image)
				if err != nil {
					return fmt.Errorf("failed to parse image: %v", err)
				}
				images = append(images, pimage)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("failed to visit resources: %v", err)
		}

	} else {
		clog.InfoContext(ctx, "visit", "args", args)
		r := builder.ResourceTypeOrNameArgs(true, args...).Flatten().Do()
		if err := r.Err(); err != nil {
			return fmt.Errorf("failed to build resource: %v", err)
		}

		kcli, err := f.KubernetesClientSet()
		if err != nil {
			return fmt.Errorf("failed to get kubernetes client: %v", err)
		}

		if err := r.Visit(func(i *resource.Info, err error) error {
			if err != nil {
				return err
			}

			clog.InfoContext(ctx, "visit", "name", i.Name, "namespace", i.Namespace, "obj", i.String())

			ns, selector, err := polymorphichelpers.SelectorsForObject(i.Object)
			if err != nil {
				return fmt.Errorf("failed to get selectors for object: %v", err)
			}

			pods, err := kcli.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
				LabelSelector: selector.String(),
			})
			if err != nil {
				return fmt.Errorf("failed to list pods: %v", err)
			}
			for _, pod := range pods.Items {
				for _, c := range pod.Spec.Containers {
					pimage, err := parseImage(c.Image)
					if err != nil {
						return fmt.Errorf("failed to parse image: %v", err)
					}
					images = append(images, pimage)
				}
			}

			return nil
		}); err != nil {
			return fmt.Errorf("failed to visit resources: %v", err)
		}
	}

	out, err := json.MarshalIndent(images, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal images: %v", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", out)

	if c.EnforceRegistry != "" {
		violations := []ParsedImage{}

		skipPattern, err := regexp.Compile(c.EnforceRegistrySkip)
		if err != nil {
			return fmt.Errorf("failed to compile skip pattern %q: %v", c.EnforceRegistrySkip, err)
		}

		for _, image := range images {
			if image.Registry != c.EnforceRegistry {
				if skipPattern.MatchString(image.Ref) {
					clog.InfoContextf(ctx, "skipping enforcement of registry %s on image ref: %q", c.EnforceRegistry, image.Ref)
					continue
				}
				violations = append(violations, image)
			}
		}

		if len(violations) > 0 {
			out, err := json.MarshalIndent(violations, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal violations: %v", err)
			}
			return fmt.Errorf("found %d images that do not belong to the enforced registry %q:\n%s", len(violations), c.EnforceRegistry, out)
		}

		clog.InfoContext(ctx, "No registry violations found", "enforced_registry", c.EnforceRegistry)
	}

	return nil
}

func parseImage(image string) (ParsedImage, error) {
	ref, err := name.ParseReference(image)
	if err != nil {
		return ParsedImage{}, fmt.Errorf("failed to parse image reference: %v", err)
	}

	return ParsedImage{
		Registry:     ref.Context().RegistryStr(),
		Repo:         ref.Context().RepositoryStr(),
		RegistryRepo: ref.Context().RegistryStr() + "/" + ref.Context().RepositoryStr(),
		Identifier:   ref.Identifier(),
		Ref:          ref.String(),
	}, nil
}

type ParsedImage struct {
	Registry     string `json:"registry"`
	Repo         string `json:"repo"`
	RegistryRepo string `json:"registry_repo"`
	Identifier   string `json:"identifier"`
	Ref          string `json:"ref"`
}
