// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package tablegenerator

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/cucumber/godog"
	"github.com/olekukonko/tablewriter"

	"github.com/redpanda-data/redpanda-operator/harpoon/internal/tracking"
)

var dryRun bool

func init() {
	flag.BoolVar(&dryRun, "dry-run", false, "dry run and print to stdout instead of writing back to the template")
}

func exitOnError(err error) {
	if err != nil {
		fmt.Printf("Error while generating table: %v", err)
		os.Exit(1)
	}
}

type supportedScenario struct {
	name               string
	supportedProviders map[string]struct{}
}

func supportedScenariosFromMap(m map[string]map[string]struct{}) []*supportedScenario {
	scenarios := []*supportedScenario{}
	for name, providers := range m {
		scenarios = append(scenarios, &supportedScenario{
			name:               name,
			supportedProviders: providers,
		})
	}
	return scenarios
}

type supportedFeature struct {
	name      string
	scenarios []*supportedScenario
}

func supportedFeaturesFromMap(m map[string]map[string]map[string]struct{}) []*supportedFeature {
	features := []*supportedFeature{}
	for name, scenarios := range m {
		features = append(features, &supportedFeature{
			name:      name,
			scenarios: supportedScenariosFromMap(scenarios),
		})
	}
	return features
}

func generateTables(providers []string, headerDepth string, features []*supportedFeature) (string, error) {
	sections := []string{}

	header := []string{"Scenario"}
	header = append(header, providers...)

	for _, feature := range features {
		var buffer bytes.Buffer

		scenarios := [][]string{}

		for _, scenario := range feature.scenarios {
			row := []string{scenario.name}
			for _, provider := range providers {
				value := ""
				if _, ok := scenario.supportedProviders[provider]; ok {
					value = "✅"
				}
				row = append(row, value)
			}
			scenarios = append(scenarios, row)
		}

		if _, err := buffer.WriteString(fmt.Sprintf("%s Feature: %s\n\n", headerDepth, feature.name)); err != nil {
			return "", err
		}

		table := tablewriter.NewWriter(&buffer)
		table.SetHeader(header)
		table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
		table.SetCenterSeparator("|")
		table.SetAutoWrapText(false)
		table.AppendBulk(scenarios)
		table.Render()

		if _, err := buffer.WriteString("\n"); err != nil {
			return "", err
		}

		sections = append(sections, buffer.String())
	}

	return strings.Join(sections, "\n"), nil
}

func RunGenerator(template, tag, headerDepth string, providers ...string) {
	flag.Parse()

	suite := &godog.TestSuite{Options: &godog.Options{}}
	features, err := suite.RetrieveFeatures()
	exitOnError(err)

	featureSet := map[string]map[string]map[string]struct{}{}

	for _, feature := range features {
		name := feature.Feature.Name
		children := feature.GherkinDocument.Feature.Children

		childSet := map[string]map[string]struct{}{}
		for _, child := range children {
			if child.Scenario == nil {
				continue
			}
			childSet[child.Scenario.Name] = map[string]struct{}{}
		}
		featureSet[name] = childSet
	}

	for _, provider := range providers {
		for _, feature := range features {
			name := feature.Feature.Name
			children := feature.GherkinDocument.Feature.Children

			children = tracking.FilterChildren(provider, children)
			for _, child := range children {
				featureSet[name][child.Scenario.Name][provider] = struct{}{}
			}
		}
	}

	tables, err := generateTables(providers, headerDepth, supportedFeaturesFromMap(featureSet))
	exitOnError(err)

	data, err := os.ReadFile(template)
	exitOnError(err)

	var buffer bytes.Buffer

	tagFound := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		_, err := buffer.WriteString(line + "\n")
		exitOnError(err)
		if line == tag {
			tagFound = true
			break
		}
	}

	if !tagFound {
		exitOnError(errors.New("unable to find tag in template"))
	}

	_, err = buffer.WriteString(tables)
	exitOnError(err)

	if dryRun {
		fmt.Println(buffer.String())
		return
	}

	os.WriteFile(template, buffer.Bytes(), 0644)
}
