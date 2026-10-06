/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Command openapi-schema-gen renders the OpenAPI v2 schema produced by
// k8s.io/kube-openapi's openapi-gen (see pkg/generated/openapi) to a JSON
// file on disk. The resulting file is fed to applyconfiguration-gen via its
// --openapi-schema flag so that generated apply configurations carry real,
// per-type structured-merge-diff schemas instead of the generic
// "__untyped_atomic_"/"__untyped_deduced_" fallbacks that applyconfiguration-gen
// emits when no schema is provided.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"k8s.io/kube-openapi/pkg/builder"
	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/util"
	"k8s.io/kube-openapi/pkg/validation/spec"

	openapi "go.goms.io/fleet-networking/pkg/generated/openapi"
)

func main() {
	outputFile := flag.String("output-file", "", "path to write the generated OpenAPI v2 schema JSON")
	flag.Parse()
	if *outputFile == "" {
		fmt.Fprintln(os.Stderr, "--output-file is required")
		os.Exit(1)
	}

	// GetDefinitionName must match the REST-friendly type name
	// (e.g. "io.goms.go.fleet-networking.api.v1alpha1.MultiClusterLoadBalancer")
	// that applyconfiguration-gen looks up by, which is derived the same way
	// via util.ToRESTFriendlyName in k8s.io/code-generator's
	// applyconfiguration-gen/generators/openapi.go.
	cfg := &common.Config{
		GetDefinitions: openapi.GetOpenAPIDefinitions,
		GetDefinitionName: func(name string) (string, spec.Extensions) {
			return util.ToRESTFriendlyName(name), nil
		},
		Info: &spec.Info{
			InfoProps: spec.InfoProps{
				Title:   "fleet-networking",
				Version: "unversioned",
			},
		},
	}

	// Collect every type name belonging to this repo's own API packages;
	// BuildOpenAPIDefinitionsForResources recursively pulls in whatever
	// external types (metav1.ObjectMeta, corev1.ServiceType, etc.) those
	// types depend on. Passing every single key from the definitions map
	// instead (including unrelated package-internal types pulled in only
	// because they share a generated-openapi package, such as
	// meta/v1.InternalEvent) fails, since some of those types reference
	// Go interfaces (e.g. runtime.Object) that have no OpenAPI schema.
	refCallback := func(name string) spec.Ref {
		return spec.MustCreateRef("#/definitions/" + util.ToRESTFriendlyName(name))
	}
	defs := openapi.GetOpenAPIDefinitions(refCallback)
	names := make([]string, 0, len(defs))
	for name := range defs {
		if strings.HasPrefix(name, "go.goms.io/fleet-networking/api/") {
			names = append(names, name)
		}
	}

	swagger, err := builder.BuildOpenAPIDefinitionsForResources(cfg, names...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build openapi schema: %v\n", err)
		os.Exit(1)
	}

	raw, err := json.MarshalIndent(swagger, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal openapi schema: %v\n", err)
		os.Exit(1)
	}
	raw = append(raw, '\n')

	if err := os.WriteFile(*outputFile, raw, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write openapi schema file: %v\n", err)
		os.Exit(1)
	}
}
