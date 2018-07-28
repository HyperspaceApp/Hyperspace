package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	fileConfig "github.com/HyperspaceApp/Hyperspace/config"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/profile"
	mnemonics "github.com/NebulousLabs/entropy-mnemonics"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh/terminal"
)

// passwordPrompt securely reads a password from stdin.
func passwordPrompt(prompt string) (string, error) {
	fmt.Print(prompt)
	pw, err := terminal.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	return string(pw), err
}

// verifyAPISecurity checks that the security values are consistent with a
// sane, secure system.
func verifyAPISecurity(config Config) error {
	// Make sure that only the loopback address is allowed unless the
	// --disable-api-security flag has been used.
	if !config.Siad.AllowAPIBind {
		addr := modules.NetAddress(config.Siad.APIaddr)
		if !addr.IsLoopback() {
			if addr.Host() == "" {
				return fmt.Errorf("a blank host will listen on all interfaces, did you mean localhost:%v?\nyou must pass --disable-api-security to bind Hsd to a non-localhost address", addr.Port())
			}
			return errors.New("you must pass --disable-api-security to bind Hsd to a non-localhost address")
		}
		return nil
	}

	// If the --disable-api-security flag is used, enforce that
	// --authenticate-api must also be used.
	if config.Siad.AllowAPIBind && !config.Siad.AuthenticateAPI {
		return errors.New("cannot use --disable-api-security without setting an api password")
	}
	return nil
}

// processNetAddr adds a ':' to a bare integer, so that it is a proper port
// number.
func processNetAddr(addr string) string {
	_, err := strconv.Atoi(addr)
	if err == nil {
		return ":" + addr
	}
	return addr
}

// processModules makes the modules string lowercase to make checking if a
// module in the string easier, and returns an error if the string contains an
// invalid module character.
func processModules(modules string) (string, error) {
	modules = strings.ToLower(modules)
	validModules := "cghmrtwepsi"
	invalidModules := modules
	for _, m := range validModules {
		invalidModules = strings.Replace(invalidModules, string(m), "", 1)
	}
	if len(invalidModules) > 0 {
		return "", errors.New("Unable to parse --modules flag, unrecognized or duplicate modules: " + invalidModules)
	}
	return modules, nil
}

// processProfileFlags checks that the flags given for profiling are valid.
func processProfileFlags(profile string) (string, error) {
	profile = strings.ToLower(profile)
	validProfiles := "cmt"

	invalidProfiles := profile
	for _, p := range validProfiles {
		invalidProfiles = strings.Replace(invalidProfiles, string(p), "", 1)
	}
	if len(invalidProfiles) > 0 {
		return "", errors.New("Unable to parse --profile flags, unrecognized or duplicate flags: " + invalidProfiles)
	}
	return profile, nil
}

// processConfig checks the configuration values and performs cleanup on
// incorrect-but-allowed values.
func processConfig(config Config) (Config, error) {
	var err1, err2 error
	config.Siad.APIaddr = processNetAddr(config.Siad.APIaddr)
	config.Siad.RPCaddr = processNetAddr(config.Siad.RPCaddr)
	config.Siad.HostAddr = processNetAddr(config.Siad.HostAddr)
	config.Siad.Modules, err1 = processModules(config.Siad.Modules)
	config.Siad.Profile, err2 = processProfileFlags(config.Siad.Profile)
	err3 := verifyAPISecurity(config)
	err := build.JoinErrors([]error{err1, err2, err3}, ", and ")
	if err != nil {
		return Config{}, err
	}
	return config, nil
}

// unlockWallet is called on hsd startup and attempts to automatically
// unlock the wallet with the given password string.
func unlockWallet(w modules.Wallet, password string) error {
	var validKeys []crypto.TwofishKey
	dicts := []mnemonics.DictionaryID{"english", "german", "japanese"}
	for _, dict := range dicts {
		seed, err := modules.StringToSeed(password, dict)
		if err != nil {
			continue
		}
		validKeys = append(validKeys, crypto.TwofishKey(crypto.HashObject(seed)))
	}
	validKeys = append(validKeys, crypto.TwofishKey(crypto.HashObject(password)))
	for _, key := range validKeys {
		if err := w.Unlock(key); err == nil {
			return nil
		}
	}
	return modules.ErrBadEncryptionKey
}

func readFileConfig(config Config) error {
	viper.SetConfigType("yaml")
	viper.SetConfigName("sia")
	viper.AddConfigPath(".")

	if strings.Contains(config.Siad.Modules, "p") {
		err := viper.ReadInConfig() // Find and read the config file
		if err != nil {             // Handle errors reading the config file
			return err
		}
		poolViper := viper.Sub("miningpool")
		poolViper.SetDefault("name", "")
		poolViper.SetDefault("id", "")
		poolViper.SetDefault("acceptingcontracts", false)
		poolViper.SetDefault("operatorpercentage", 0.0)
		poolViper.SetDefault("operatorwallet", "")
		poolViper.SetDefault("networkport", 6666)
		poolViper.SetDefault("dbaddress", "127.0.0.1")
		poolViper.SetDefault("dbname", "miningpool")
		poolViper.SetDefault("dbport", "3306")
		if !poolViper.IsSet("poolwallet") {
			return errors.New("Must specify a poolwallet")
		}
		if !poolViper.IsSet("dbuser") {
			return errors.New("Must specify a dbuser")
		}
		if !poolViper.IsSet("dbpass") {
			return errors.New("Must specify a dbpass")
		}
		dbUser := poolViper.GetString("dbuser")
		dbPass := poolViper.GetString("dbpass")
		dbAddress := poolViper.GetString("dbaddress")
		dbPort := poolViper.GetString("dbport")
		dbName := poolViper.GetString("dbname")
		dbConnection := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", dbUser, dbPass, dbAddress, dbPort, dbName)
		poolConfig := fileConfig.MiningPoolConfig{
			PoolNetworkPort:  int(poolViper.GetInt("networkport")),
			PoolName:         poolViper.GetString("name"),
			PoolID:           uint64(poolViper.GetInt("id")),
			PoolDBConnection: dbConnection,
			PoolWallet:       poolViper.GetString("poolwallet"),
		}
		globalConfig.MiningPoolConfig = poolConfig
	}
	if strings.Contains(config.Siad.Modules, "i") {
		err := viper.ReadInConfig() // Find and read the config file
		if err != nil {             // Handle errors reading the config file
			return err
		}
		poolViper := viper.Sub("index")
		poolViper.SetDefault("dbaddress", "127.0.0.1")
		poolViper.SetDefault("dbname", "siablocks")
		poolViper.SetDefault("dbport", "3306")
		if !poolViper.IsSet("dbuser") {
			return errors.New("Must specify a dbuser")
		}
		if !poolViper.IsSet("dbpass") {
			return errors.New("Must specify a dbpass")
		}
		dbUser := poolViper.GetString("dbuser")
		dbPass := poolViper.GetString("dbpass")
		dbAddress := poolViper.GetString("dbaddress")
		dbPort := poolViper.GetString("dbport")
		dbName := poolViper.GetString("dbname")
		dbConnection := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", dbUser, dbPass, dbAddress, dbPort, dbName)
		globalConfig.IndexConfig = fileConfig.IndexConfig{
			PoolDBConnection: dbConnection,
		}
	}
	return nil
}

// startDaemon uses the config parameters to initialize Sia modules and start
// hsd.
func startDaemon(config Config) (err error) {
	if config.Siad.AuthenticateAPI {
		password := os.Getenv("HYPERSPACE_API_PASSWORD")
		if password != "" {
			fmt.Println("Using HYPERSPACE_API_PASSWORD environment variable")
			config.APIPassword = password
		} else {
			// Prompt user for API password.
			config.APIPassword, err = passwordPrompt("Enter API password: ")
			if err != nil {
				return err
			}
			if config.APIPassword == "" {
				return errors.New("password cannot be blank")
			}
		}
	}

	// Print the hsd Version and GitRevision
	fmt.Println("Hyperspace Daemon v" + build.Version)
	if build.GitRevision == "" {
		fmt.Println("WARN: compiled without build commit or version. To compile correctly, please use the makefile")
	} else {
		fmt.Println("Git Revision " + build.GitRevision)
	}

	// Install a signal handler that will catch exceptions thrown by mmap'd
	// files.
	// NOTE: ideally we would catch SIGSEGV here too, since that signal can
	// also be thrown by an mmap I/O error. However, SIGSEGV can occur under
	// other circumstances as well, and in those cases, we will want a full
	// stack trace.
	mmapChan := make(chan os.Signal, 1)
	signal.Notify(mmapChan, syscall.SIGBUS)
	go func() {
		<-mmapChan
		fmt.Println("A fatal I/O exception (SIGBUS) has occurred.")
		fmt.Println("Please check your disk for errors.")
		os.Exit(1)
	}()

	// Print a startup message.
	fmt.Println("Loading...")
	loadStart := time.Now()
	srv, err := NewServer(config)
	if err != nil {
		return err
	}
	errChan := make(chan error)
	go func() {
		errChan <- srv.Serve()
	}()
	err = srv.loadModules()
	if err != nil {
		return err
	}

	// listen for kill signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, os.Kill, syscall.SIGTERM)

	// Print a 'startup complete' message.
	startupTime := time.Since(loadStart)
	fmt.Println("Finished loading in", startupTime.Seconds(), "seconds")

	// wait for Serve to return or for kill signal to be caught
	err = func() error {
		select {
		case err := <-errChan:
			return err
		case <-sigChan:
			fmt.Println("\rCaught stop signal, quitting...")
			return srv.Close()
		}
	}()
	if err != nil {
		build.Critical(err)
	}

	return nil
}

// startDaemonCmd is a passthrough function for startDaemon.
func startDaemonCmd(cmd *cobra.Command, _ []string) {
	var profileCPU, profileMem, profileTrace bool

	configErr := readFileConfig(globalConfig)
	if configErr != nil {
		fmt.Println("Configuration error: ", configErr.Error())
		os.Exit(exitCodeGeneral)
	}

	profileCPU = strings.Contains(globalConfig.Siad.Profile, "c")
	profileMem = strings.Contains(globalConfig.Siad.Profile, "m")
	profileTrace = strings.Contains(globalConfig.Siad.Profile, "t")

	if build.DEBUG {
		profileCPU = true
		profileMem = true
	}

	if profileCPU || profileMem || profileTrace {
		go profile.StartContinuousProfile(globalConfig.Siad.ProfileDir, profileCPU, profileMem, profileTrace)
	}

	// Start hsd. startDaemon will only return when it is shutting down.
	err := startDaemon(globalConfig)
	if err != nil {
		die(err)
	}

	// Daemon seems to have closed cleanly. Print a 'closed' mesasge.
	fmt.Println("Shutdown complete.")
}
