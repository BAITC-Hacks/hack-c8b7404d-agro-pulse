package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"Agro-Pulse/internal/demo"
	"Agro-Pulse/internal/loader"
	"Agro-Pulse/internal/model"
	"Agro-Pulse/internal/normalize"
	"Agro-Pulse/internal/pipeline"
)

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("agropulse", flag.ContinueOnError)
	fs.SetOutput(stderr)
	iek := fs.String("iek-dir", "", "directory with the six original IEK XLSX files")
	asof := fs.String("as-of", "", "required data snapshot date YYYY-MM-DD")
	months := fs.Int("months", 1, "MVP ASSUMPTION: forecast horizon in calendar months (1..24)")
	output := fs.String("output", "", "save the JSON report to a new file (existing files are never overwritten)")
	demoMode := fs.Bool("demo", false, "run five isolated synthetic_demo scenarios; cannot be combined with real inputs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *demoMode {
		mixed := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name != "demo" && f.Name != "output" {
				mixed = true
			}
		})
		if mixed {
			return fmt.Errorf("--demo cannot be combined with real input paths, dates or horizon overrides")
		}
		report, err := demo.Run()
		if err != nil {
			return err
		}
		if *output != "" {
			f, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			encodeErr := enc.Encode(report)
			closeErr := f.Close()
			if encodeErr != nil {
				return encodeErr
			}
			return closeErr
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	d, err := normalize.Date(*asof)
	if err != nil {
		return err
	}
	if *months < 1 || *months > 24 {
		return fmt.Errorf("months must be 1..24")
	}
	if *iek == "" {
		return fmt.Errorf("--iek-dir is required")
	}
	cfg := model.Config{AsOf: d, Months: *months}
	dataset, err := loader.LoadIEK(*iek)
	if err != nil {
		return err
	}
	report := model.Report{MethodVersion: "mvp-2-strict-iek", Config: cfg, Assumptions: pipeline.Assumptions, Suppliers: []model.SupplierReport{pipeline.Run(dataset, cfg)}}
	if *output != "" {
		f, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(f)
		encodeErr := enc.Encode(report)
		closeErr := f.Close()
		if encodeErr != nil {
			return encodeErr
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(stderr, "IEK report: %s; SKU=%d; matched_all_sources=%d\n", *output, len(dataset.Products), len(dataset.Quality.MatchedAllSources))
		return nil
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "agropulse:", err)
		os.Exit(1)
	}
}
