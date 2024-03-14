package main

import (
	"flag"
	"github.com/gravitational/trace"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	helmCRDPrefix = `{{/*
  Deploy the CRD if 'installCRDs' is set to "always", or if 'installCRD' is set
  to "dynamic" and either 'enabled' is true or the CRD is already present.
*/}}
{{- if or
  (eq .Values.installCRDs "always")
  (and
    (eq .Values.installCRDs "dynamic")
    (or
      .Values.enabled
      (lookup "apiextensions.k8s.io/v1" "CustomResourceDefinition" "" "teleportgithubconnectors.resources.teleport.dev")
    )
  )
}}
`
	helmCRDSuffix = `{{- end }}
`
)

func main() {
	var sourceDir string
	var destDir string
	flag.StringVar(&sourceDir, "source", "", "Source directory containing the CRDs.")
	flag.StringVar(&destDir, "destination", "", "Destination directory, the Helm chart template directory.")
	flag.Parse()

	if sourceDir == "" {
		log.Fatalln("source flag must be specified")
	}
	if destDir == "" {
		log.Fatalln("destination flag must be specified")
	}
	err := run(sourceDir, destDir)
	if err != nil {
		log.Fatalln(err)
	}

}

func run(sourceDir, destDir string) error {
	crds, err := readCRDs(sourceDir)
	if err != nil {
		return trace.Wrap(err)
	}

	err = writeCrds(destDir, crds)
	if err != nil {
		return trace.Wrap(err)
	}

	log.Printf("%d CRDs written\n", len(crds))
	return nil
}

func readCRDs(sourceDir string) (map[string][]byte, error) {
	crds := make(map[string][]byte)

	f, err := os.Open(sourceDir)
	defer f.Close()

	if err != nil {
		return nil, trace.Errorf("failed to open source directory %q: %s", sourceDir, err)
	}
	files, err := f.Readdir(0)
	if err != nil {
		return nil, trace.Errorf("failed to list files in source directory %q: %s", sourceDir, err)
	}

	for _, v := range files {
		if v.IsDir() || !strings.HasSuffix(v.Name(), ".yaml") {
			continue
		}
		fullPath := filepath.Join(sourceDir, v.Name())
		log.Printf("reading CRD file %q\n", fullPath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, trace.Errorf("failed to read CRD %q: %s", fullPath, err)
		}
		crds[v.Name()] = content
	}
	return crds, nil
}

func craftHelmCRD(originalCRD string) (string, error) {
	sb := strings.Builder{}
	_, err := sb.WriteString(helmCRDPrefix)
	if err != nil {
		return "", trace.Wrap(err)
	}

	escapedCRD := strings.ReplaceAll(originalCRD, "`{{", "{{ `{{")
	escapedCRD = strings.ReplaceAll(escapedCRD, "}}`", "}}` }}")
	_, err = sb.WriteString(escapedCRD)
	if err != nil {
		return "", trace.Wrap(err)
	}

	_, err = sb.WriteString(helmCRDSuffix)
	if err != nil {
		return "", trace.Wrap(err)
	}

	return sb.String(), nil
}

func writeCrds(destDir string, crds map[string][]byte) error {
	for crdName, crdContent := range crds {
		helmCRDContent, err := craftHelmCRD(string(crdContent))
		if err != nil {
			return trace.Errorf("failed to craft template for CRD %q: %s", crdName, err)
		}
		fullPath := filepath.Join(destDir, crdName)
		log.Printf("writing CRD file %q\n", fullPath)
		err = os.WriteFile(fullPath, []byte(helmCRDContent), 0644)
		if err != nil {
			return trace.Errorf("failed to write file for CRD tempalte %s: %q", fullPath, err)
		}
	}
	return nil
}
