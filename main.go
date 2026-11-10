// mayoi-java prepares a JDT LS launch description, then exits. Mayoi owns LSP.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

var version = "0.3.0-dev"

func run(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mayoi-java resolve|doctor|setup|install-lombok|version [--root PATH]")
	}
	if args[0] == "version" {
		fmt.Fprintln(out, version)
		return nil
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	root := flags.String("root", "", "workspace path (default: current directory)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	settings := map[string]json.RawMessage{}
	if args[0] == "resolve" {
		var request struct {
			Root     string                     `json:"root"`
			Settings map[string]json.RawMessage `json:"settings"`
		}
		data, err := io.ReadAll(io.LimitReader(in, (1<<20)+1))
		if err != nil {
			return err
		}
		if len(data) > 1<<20 {
			return fmt.Errorf("resolver input exceeds 1 MiB")
		}
		if err = decodeStrict(data, &request); err != nil {
			return err
		}
		if request.Root == "" {
			return fmt.Errorf("resolver requires root")
		}
		*root, settings = request.Root, request.Settings
	}
	if *root == "" {
		var err error
		*root, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	switch args[0] {
	case "setup", "install-lombok":
		if err := installLombok(); err != nil {
			return err
		}
		if args[0] == "install-lombok" {
			fmt.Fprintln(out, lombokPath())
			return nil
		}
	case "resolve", "doctor":
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	plan, err := resolve(*root, settings)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mayoi-java:", err)
		os.Exit(1)
	}
}
