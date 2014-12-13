package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"regexp"

	"strings"

	"sourcegraph.com/sourcegraph/srclib-docker/dockerfile"
	"sourcegraph.com/sourcegraph/srclib/graph"
	"sourcegraph.com/sourcegraph/srclib/grapher"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

// The Def.Path and Ref.DefPath of a Dockerfile. There is 1 Dockerfile
// per source unit, so each Dockerfile is at that source unit's "."
// path.
const DockerfileDefPath = "."

func init() {
	_, err := parser.AddCommand("graph",
		"graph a Dockerfile",
		"Graph a Dockerfile.",
		&graphCmd,
	)
	if err != nil {
		log.Fatal(err)
	}
}

type GraphCmd struct{}

var graphCmd GraphCmd

func (c *GraphCmd) Execute(args []string) error {
	var unit *unit.SourceUnit
	if err := json.NewDecoder(os.Stdin).Decode(&unit); err != nil {
		return err
	}
	if err := os.Stdin.Close(); err != nil {
		return err
	}

	dfpath := unit.Files[0]
	data, err := ioutil.ReadFile(dfpath)
	if err != nil {
		return err
	}
	dfJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	var o grapher.Output

	// Add def.
	o.Defs = append(o.Defs, &graph.Def{
		DefKey:   graph.DefKey{Path: DockerfileDefPath},
		TreePath: ".",
		Name:     "Dockerfile",
		File:     dfpath,
		Exported: true,

		DefStart: 0,
		DefEnd:   len(data) - 1,

		Data: dfJSON,
	})

	// Add refs.
	dataLCStr := string(bytes.ToLower(data))
	df, err := dockerfile.Decode(bytes.NewReader(data))
	if err == nil {
		// Add ref to base image.
		if df.From != "" {
			_, _, _, _, err := resolveImageRef(df.From)
			if err == nil {
				start, end := findStartEnd(dataLCStr, df.From)
				if start != -1 {
					o.Refs = append(o.Refs, &graph.Ref{
						DefRepo:     DockerImageDynRefCloneURL,
						DefUnitType: DockerfileUnitType,
						DefUnit:     df.From,
						DefPath:     ".",
						File:        dfpath,
						Start:       start,
						End:         end,
					})
				}
			} else {
				log.Printf("Error parsing image ref %q: %s. Skipping ref.", df.From, err)
			}
		}

		// Add refs to things that look like repo URIs.
		uriIdxs := repoURIPat.FindAllStringIndex(dataLCStr, -1)
		for _, m := range uriIdxs {
			start, end := m[0], m[1]
			uri := string(data[start:end])
			o.Refs = append(o.Refs, &graph.Ref{
				DefRepo:     uri,
				DefUnitType: DirectRepoLinkUnitType,
				DefUnit:     ".",
				File:        dfpath,
				Start:       start,
				End:         end,
			})
		}

		instrIdxs := instructionPat.FindAllStringIndex(dataLCStr, -1)
		for _, m := range instrIdxs {
			start, end := m[0], m[1]
			instruction := string(data[start:end])
			o.Refs = append(o.Refs, &graph.Ref{
				DefRepo:     "github.com/docker/docker",
				DefUnitType: DirectURLLinkUnitType,
				DefUnit:     ".",
				DefPath:     graph.DefPath("https://docs.docker.com/reference/builder/#" + strings.ToLower(instruction)),
				File:        dfpath,
				Start:       start,
				End:         end,
			})
		}
	} else {
		log.Printf("Error parsing Dockerfile %q: %s. Skipping refs.", dfpath, err)
	}

	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(b); err != nil {
		return err
	}
	fmt.Println()
	return nil
}

// findStartEnd fuzzily (case-insensitively) finds the start and end
// character positions of substr in s. If substr is not in s, -1 is returned for both start and end.
func findStartEnd(s, substr string) (start, end int) {
	substr = strings.ToLower(substr)
	i := strings.Index(s, substr)
	if i == -1 {
		return -1, -1
	}
	return i, i + len(substr)
}

var (
	repoURIPat     = regexp.MustCompile(`(?:github\.com|sourcegraph\.com)/[\w.-]+/[\w.-]+`)
	instructionPat = regexp.MustCompile(`(?m:^(from|maintainer|run|cmd|expose|env|add|copy|entrypoint|volume|user|workdir|onbuild))`)
)
