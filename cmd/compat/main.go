package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"

	"github.com/kcp-dev/kcp/pkg/schemacompat"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Determine schema compatibility of two CRD YAMLs

Usage:
	compat old-crd.yaml new-crd.yaml

Flags:
`)
		flag.PrintDefaults()
	}
	var lcd = flag.Bool("lcd", false, "If true, print LCD YAML to stdout")

	flag.Parse()
	if len(flag.Args()) != 2 {
		log.Fatalf("Expected exactly two args: old, new")
	}
	oldfile, newfile := flag.Args()[0], flag.Args()[1]

	old, err := parse(oldfile)
	if err != nil {
		log.Fatal(err)
	}
	new, err := parse(newfile)
	if err != nil {
		log.Fatal(err)
	}

	out, err := schemacompat.EnsureStructuralSchemaCompatibility(
		field.NewPath(""),
		// TODO: take flags for desired versions, instead of just assuming the first.
		old.Spec.Versions[0].Schema.OpenAPIV3Schema,
		new.Spec.Versions[0].Schema.OpenAPIV3Schema,
		*lcd)
	if err != nil {
		log.Fatal(err)
	}

	if *lcd {
		old.Spec.Versions[0].Schema.OpenAPIV3Schema = out
		b, err := yaml.Marshal(old)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := io.Copy(os.Stdout, bytes.NewReader(b)); err != nil {
			log.Fatal(err)
		}
	}
}

func parse(fn string) (*apiextensionsv1.CustomResourceDefinition, error) {
	b, err := ioutil.ReadFile(fn)
	if err != nil {
		log.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	return &crd, yaml.Unmarshal(b, &crd)
}
