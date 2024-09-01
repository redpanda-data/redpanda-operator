// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"text/template"
	"time"

	"golang.org/x/sync/errgroup"
)

type templateData struct {
	Year int
}

var (
	embeddedLicenses = []string{"mit", "bsl", "rcl"}
	//go:embed templates/*
	licenses embed.FS

	shortLicenseHeaderTemplates = map[string]*template.Template{}
	licenseHeaderTemplates      = map[string]*template.Template{}
	licenseTemplates            = map[string]*template.Template{}

	licenseTemplateData = &templateData{
		Year: time.Now().Year(),
	}

	writer fileWriter = &fsWriter{}
)

func init() {
	for _, embedded := range embeddedLicenses {
		shortHeader := template.Must(template.New("").ParseFS(licenses, "*/"+shortHeaderName(embedded)))
		header := template.Must(template.New("").ParseFS(licenses, "*/"+headerName(embedded)))
		full := template.Must(template.New("").ParseFS(licenses, "*/"+fullName(embedded)))

		shortLicenseHeaderTemplates[embedded] = shortHeader
		licenseHeaderTemplates[embedded] = header
		licenseTemplates[embedded] = full
	}
}

func dieOnError(err error) {
	if err != nil {
		log.Printf("error processing files: %v", err)
		os.Exit(1)
	}
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", ".licenseupdater.yaml", "path to config file")

	flag.Parse()

	config, err := loadConfig(configFile)
	dieOnError(err)
	dieOnError(doMain(config))
}

func doMain(config *config) error {
	var group errgroup.Group

	ch := make(chan *matchedFile, 1000)
	group.Go(func() error {
		return config.walk(ch)
	})

	for f := range ch {
		group.Go(f.process)
	}

	if err := group.Wait(); err != nil {
		return err
	}

	if err := config.writeTopLevelLicense(); err != nil {
		return err
	}

	if err := config.writeLicenses(); err != nil {
		return err
	}

	return config.writeStaticFiles()
}

func shortHeaderName(name string) string {
	name = strings.ToUpper(name)
	return fmt.Sprintf("%s.short", name)
}

func headerName(name string) string {
	name = strings.ToUpper(name)
	return fmt.Sprintf("%s.header.tmpl", name)
}

func fullName(name string) string {
	name = strings.ToUpper(name)
	return fmt.Sprintf("%s.md.tmpl", name)
}
