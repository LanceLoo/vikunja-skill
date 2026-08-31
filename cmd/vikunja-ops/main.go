package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("vikunja-ops", flag.ContinueOnError)
	flags.SetOutput(stderr)
	// Help is defined explicitly so all accepted top-level help forms use stdout
	// without flag's automatic ErrHelp output.
	flags.Usage = func() {}
	showHelp := flags.Bool("help", false, "输出帮助")
	showHelpShort := flags.Bool("h", false, "输出帮助")
	pretty := flags.Bool("pretty", false, "以两空格缩进输出 JSON")
	showVersion := flags.Bool("version", false, "输出版本")
	if err := flags.Parse(args); err != nil {
		printUsage(stderr)
		return 2
	}
	if *showHelp || *showHelpShort {
		printUsage(stdout)
		return 0
	}
	remaining := flags.Args()
	if *showVersion {
		if len(remaining) != 0 {
			fmt.Fprintln(stderr, "--version must be used without a command or arguments")
			printUsage(stderr)
			return 2
		}
		printVersion(stdout)
		return 0
	}
	if len(remaining) == 0 {
		printUsage(stdout)
		return 0
	}
	if *pretty {
		stdout = prettyJSONWriter{Writer: stdout}
	}
	switch remaining[0] {
	case "projects":
		return runProjects(remaining[1:], stdout, stderr)
	case "labels":
		return runLabels(remaining[1:], stdout, stderr)
	case "tasks":
		return runTasks(remaining[1:], stdout, stderr)
	case "doctor":
		return runDoctor(remaining[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", remaining[0])
		printUsage(stderr)
		return 2
	}
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		if len(args) == 1 && args[0] == "--help" {
			printDoctorUsage(stdout)
			return 0
		}
		printDoctorUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "doctor: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "配置有效")
	c := client.New(nil)
	if err := c.CheckOpenAPI(cfg.BaseURL); err != nil {
		fmt.Fprintf(stderr, "doctor: OpenAPI 不可访问: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "OpenAPI 可访问")
	if err := c.VerifyTokenV1(cfg.BaseURL, cfg.Token); err != nil {
		switch {
		case errors.Is(err, client.ErrUnauthorized):
			fmt.Fprintln(stderr, "doctor: Token 验证失败：服务未接受 Token")
		case errors.Is(err, client.ErrForbidden):
			fmt.Fprintln(stderr, "doctor: Token 验证失败：Token 已被识别，但被实例或权限策略拒绝")
		default:
			fmt.Fprintf(stderr, "doctor: Token 验证失败: %v\n", err)
		}
		return 1
	}
	fmt.Fprintln(stdout, "Token 验证通过")
	if err := c.CheckProjectsRead(cfg.BaseURL, cfg.Token); err != nil {
		switch {
		case errors.Is(err, client.ErrUnauthorized):
			fmt.Fprintln(stderr, "doctor: v2 项目读取失败：Token 本身有效，但缺少或未匹配 v2 projects 读取权限，或服务存在兼容性问题")
		case errors.Is(err, client.ErrForbidden):
			fmt.Fprintln(stderr, "doctor: v2 项目读取失败：Token 已被识别，但项目读取被实例或权限策略拒绝")
		default:
			fmt.Fprintf(stderr, "doctor: v2 项目读取失败: %v\n", err)
		}
		return 1
	}
	fmt.Fprintln(stdout, "v2 项目读取权限可用")
	return 0
}
