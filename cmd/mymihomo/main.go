package main

import (
	"flag"
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if err := loadPersistedEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 加载持久化配置失败: %v\n", err)
	}

	// 子命令
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "download":
		downloadCmd := flag.NewFlagSet("download", flag.ExitOnError)
		confFile := downloadCmd.String("o", "/root/conf/config.yaml", "配置文件输出路径")
		downloadCmd.Parse(os.Args[2:])
		if err := downloadConfig(*confFile); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}

	case "update":
		updateCmd := flag.NewFlagSet("update", flag.ExitOnError)
		confFile := updateCmd.String("c", getEnvDefault("CONF_FILE", "/root/conf/config.yaml"), "配置文件路径")
		updateCmd.Parse(os.Args[2:])
		if err := updateConfig(*confFile); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}

	case "prepare":
		if err := prepareManagedConfig(getEnvDefault("CONF_FILE", "/root/conf/config.yaml")); err != nil {
			fmt.Fprintf(os.Stderr, "初始化失败: %s\n", safeMessage(err))
			os.Exit(1)
		}

	case "version":
		fmt.Printf("mymihomo %s\n", version)

	case "serve":
		serveCmd := flag.NewFlagSet("serve", flag.ExitOnError)
		addr := serveCmd.String("addr", ":"+getEnvDefault("WEB_CONFIG_PORT", "9097"), "配置服务监听地址")
		confFile := serveCmd.String("c", getEnvDefault("CONF_FILE", "/root/conf/config.yaml"), "配置文件路径")
		envFile := serveCmd.String("e", getWebEnvFile(), "持久化环境变量文件")
		serveCmd.Parse(os.Args[2:])
		if err := serveConfigAPI(*addr, *confFile, *envFile); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}

	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("用法: mymihomo <命令> [选项]")
	fmt.Println()
	fmt.Println("命令:")
	fmt.Println("  download  下载并处理配置文件")
	fmt.Println("  update    更新运行中的mihomo配置")
	fmt.Println("  prepare   初始化订阅存储并生成经过校验的运行配置")
	fmt.Println("  version   显示版本信息")
	fmt.Println("  serve     启动配置 API 服务")
}
