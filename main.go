package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

type Server struct {
	IP   string `yaml:"ip"`
	User string `yaml:"user"`
}

type ServerConfig map[string]Server

var (
	configFile = "config/config.yaml"
	servers    ServerConfig
)

func getTerminalSize() (int, int, error) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80, 40, err // 기본값으로 fallback
	}
	return width, height, nil
}

func getConfigFile() string {
	var configFile string
	flag.StringVar(&configFile, "config", "", "Path to the configuration file")
	flag.Parse()
	if configFile != "" {
		return configFile
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}
	if homeDir != "" {
		configFile = filepath.Join(homeDir, ".config", "conn", "config.yaml")
		return configFile
	}

	return "config/config.yaml"
}

func loadConfig() {
	data, err := os.ReadFile(getConfigFile())
	if err != nil {
		log.Fatalf("Failed to load config file: %v", err)
	}
	if err := yaml.Unmarshal(data, &servers); err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}
}

func promptPassword() string {
	fmt.Print("Enter password: ")
	bytePassword, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		log.Fatalf("Failed to read password: %v", err)
	}
	fmt.Println()
	return string(bytePassword)
}

func connect(serverName string) {
	server, exists := servers[serverName]
	if !exists {
		fmt.Printf("Server %s not found in configuration.\n", serverName)
		return
	}

	password := promptPassword()

	config := &ssh.ClientConfig{
		User:            server.User,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // For simplicity; replace with proper validation in production
		Timeout:         5 * time.Second,
	}

	address := net.JoinHostPort(server.IP, "22")
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		log.Fatalf("Failed to connect to server %s: %v", serverName, err)
	}
	defer client.Close()

	fmt.Printf("Connected to %s (%s)\n", serverName, server.IP)

	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("Failed to create SSH session: %v", err)
	}
	defer session.Close()

	// 터미널 상태 저장
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Fatalf("Failed to set terminal to raw mode: %v", err)
	}
	defer func() {
		if err := term.Restore(int(os.Stdin.Fd()), oldState); err != nil {
			log.Printf("Failed to restore terminal state: %v", err)
		}
	}()

	width, height, err := getTerminalSize()
	if err != nil {
		log.Printf("Failed to get terminal size, using default: %v", err)
	}

	err = session.RequestPty("xterm-256color", height, width, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400, // Input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // Output speed = 14.4kbaud
	})
	if err != nil {
		log.Fatalf("Failed to set terminal mode: %v", err)
	}

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	session.Stdin = os.Stdin

	err = session.Shell()
	if err != nil {
		log.Fatalf("Failed to start shell: %v", err)
	}

	err = session.Wait()
	if err != nil {
		log.Fatalf("Session ended with error: %v", err)
	}
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "conn",
		Short: "SSH CLI Tool",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			loadConfig()
		},
	}

	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Path to the configuration file")

	var connectCmd = &cobra.Command{
		Use:   "connect [server]",
		Short: "Connect to a server",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			connect(args[0])
		},
	}

	var listCmd = &cobra.Command{
		Use:   "list",
		Short: "List all available servers",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Available servers:")

			serverNames := make([]string, 0, len(servers))
			for name := range servers {
				serverNames = append(serverNames, name)
			}

			sort.Strings(serverNames)

			for _, name := range serverNames {
				server := servers[name]
				fmt.Printf("- %s (IP: %s, User: %s)\n", name, server.IP, server.User)
			}
		},
	}

	rootCmd.AddCommand(connectCmd, listCmd)
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Command execution failed: %v", err)
	}
}
