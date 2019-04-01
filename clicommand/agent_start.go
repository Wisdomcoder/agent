package clicommand

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/experiments"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/metrics"
	"github.com/buildkite/shellwords"
	"github.com/urfave/cli"
)

var StartDescription = `Usage:

   buildkite-agent start [arguments...]

Description:

   When a job is ready to run it will call the "bootstrap-script"
   and pass it all the environment variables required for the job to run.
   This script is responsible for checking out the code, and running the
   actual build script defined in the pipeline.

   The agent will run any jobs within a PTY (pseudo terminal) if available.

Example:

   $ buildkite-agent start --token xxx`

// Adding config requires changes in a few different spots
// - The AgentStartConfig struct with a cli parameter
// - As a flag in the AgentStartCommand (with matching env)
// - Into an env to be passed to the bootstrap in agent/job_runner.go, createEnvironment()
// - Into clicommand/bootstrap.go to read it from the env into the bootstrap config

type AgentStartConfig struct {
	Config                     string   `cli:"config"`
	Name                       string   `cli:"name"`
	Priority                   string   `cli:"priority"`
	DisconnectAfterJob         bool     `cli:"disconnect-after-job"`
	DisconnectAfterJobTimeout  int      `cli:"disconnect-after-job-timeout"`
	DisconnectAfterIdleTimeout int      `cli:"disconnect-after-idle-timeout"`
	BootstrapScript            string   `cli:"bootstrap-script" normalize:"commandpath"`
	CancelGracePeriod          int      `cli:"cancel-grace-period"`
	BuildPath                  string   `cli:"build-path" normalize:"filepath" validate:"required"`
	HooksPath                  string   `cli:"hooks-path" normalize:"filepath"`
	PluginsPath                string   `cli:"plugins-path" normalize:"filepath"`
	Shell                      string   `cli:"shell"`
	Tags                       []string `cli:"tags" normalize:"list"`
	TagsFromEC2                bool     `cli:"tags-from-ec2"`
	TagsFromEC2Tags            bool     `cli:"tags-from-ec2-tags"`
	TagsFromGCP                bool     `cli:"tags-from-gcp"`
	TagsFromGCPLabels          bool     `cli:"tags-from-gcp-labels"`
	TagsFromHost               bool     `cli:"tags-from-host"`
	WaitForEC2TagsTimeout      string   `cli:"wait-for-ec2-tags-timeout"`
	WaitForGCPLabelsTimeout    string   `cli:"wait-for-gcp-labels-timeout"`
	GitCloneFlags              string   `cli:"git-clone-flags"`
	GitCloneMirrorFlags        string   `cli:"git-clone-mirror-flags"`
	GitCleanFlags              string   `cli:"git-clean-flags"`
	GitMirrorsPath             string   `cli:"git-mirrors-path" normalize:"filepath"`
	GitMirrorsLockTimeout      int      `cli:"git-mirrors-lock-timeout"`
	NoGitSubmodules            bool     `cli:"no-git-submodules"`
	NoSSHKeyscan               bool     `cli:"no-ssh-keyscan"`
	NoCommandEval              bool     `cli:"no-command-eval"`
	NoLocalHooks               bool     `cli:"no-local-hooks"`
	NoPlugins                  bool     `cli:"no-plugins"`
	NoPluginValidation         bool     `cli:"no-plugin-validation"`
	NoPTY                      bool     `cli:"no-pty"`
	TimestampLines             bool     `cli:"timestamp-lines"`
	MetricsDatadog             bool     `cli:"metrics-datadog"`
	MetricsDatadogHost         string   `cli:"metrics-datadog-host"`
	Spawn                      int      `cli:"spawn"`
	LogFormat                  string   `cli:"log-format"`

	// Global flags
	Debug       bool     `cli:"debug"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`

	// API config
	DebugHTTP bool   `cli:"debug-http"`
	Token     string `cli:"token" validate:"required"`
	Endpoint  string `cli:"endpoint" validate:"required"`
	NoHTTP2   bool   `cli:"no-http2"`

	// Deprecated
	NoSSHFingerprintVerification bool     `cli:"no-automatic-ssh-fingerprint-verification" deprecated-and-renamed-to:"NoSSHKeyscan"`
	MetaData                     []string `cli:"meta-data" deprecated-and-renamed-to:"Tags"`
	MetaDataEC2                  bool     `cli:"meta-data-ec2" deprecated-and-renamed-to:"TagsFromEC2"`
	MetaDataEC2Tags              bool     `cli:"meta-data-ec2-tags" deprecated-and-renamed-to:"TagsFromEC2Tags"`
	MetaDataGCP                  bool     `cli:"meta-data-gcp" deprecated-and-renamed-to:"TagsFromGCP"`
}

func DefaultShell() string {
	// https://github.com/golang/go/blob/master/src/go/build/syslist.go#L7
	switch runtime.GOOS {
	case "windows":
		return `C:\Windows\System32\CMD.exe /S /C`
	case "freebsd", "openbsd", "netbsd":
		return `/usr/local/bin/bash -e -c`
	default:
		return `/bin/bash -e -c`
	}
}

func DefaultConfigFilePaths() (paths []string) {
	// Toggle beetwen windows an *nix paths
	if runtime.GOOS == "windows" {
		paths = []string{
			"C:\\buildkite-agent\\buildkite-agent.cfg",
			"$USERPROFILE\\AppData\\Local\\buildkite-agent\\buildkite-agent.cfg",
			"$USERPROFILE\\AppData\\Local\\BuildkiteAgent\\buildkite-agent.cfg",
		}
	} else {
		paths = []string{
			"$HOME/.buildkite-agent/buildkite-agent.cfg",
			"/usr/local/etc/buildkite-agent/buildkite-agent.cfg",
			"/etc/buildkite-agent/buildkite-agent.cfg",
		}
	}

	// Also check to see if there's a buildkite-agent.cfg in the folder
	// that the binary is running in.
	pathToBinary, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err == nil {
		pathToRelativeConfig := filepath.Join(pathToBinary, "buildkite-agent.cfg")
		paths = append([]string{pathToRelativeConfig}, paths...)
	}

	return
}

var AgentStartCommand = cli.Command{
	Name:        "start",
	Usage:       "Starts a Buildkite agent",
	Description: StartDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "config",
			Value:  "",
			Usage:  "Path to a configuration file",
			EnvVar: "BUILDKITE_AGENT_CONFIG",
		},
		cli.StringFlag{
			Name:   "name",
			Value:  "",
			Usage:  "The name of the agent",
			EnvVar: "BUILDKITE_AGENT_NAME",
		},
		cli.StringFlag{
			Name:   "priority",
			Value:  "",
			Usage:  "The priority of the agent (higher priorities are assigned work first)",
			EnvVar: "BUILDKITE_AGENT_PRIORITY",
		},
		cli.BoolFlag{
			Name:   "disconnect-after-job",
			Usage:  "Disconnect the agent after running a job",
			EnvVar: "BUILDKITE_AGENT_DISCONNECT_AFTER_JOB",
		},
		cli.IntFlag{
			Name:   "disconnect-after-job-timeout",
			Value:  120,
			Usage:  "When --disconnect-after-job is specified, the number of seconds to wait for a job before shutting down",
			EnvVar: "BUILDKITE_AGENT_DISCONNECT_AFTER_JOB_TIMEOUT",
		},
		cli.IntFlag{
			Name:   "disconnect-after-idle-timeout",
			Value:  0,
			Usage:  "If no jobs have come in for the specified number of secconds, disconnect the agent",
			EnvVar: "BUILDKITE_AGENT_DISCONNECT_AFTER_IDLE_TIMEOUT",
		},
		cli.IntFlag{
			Name:   "cancel-grace-period",
			Value:  10,
			Usage:  "The number of seconds running processes are given to gracefully terminate before they are killed when a job is cancelled",
			EnvVar: "BUILDKITE_CANCEL_GRACE_PERIOD",
		},
		cli.StringFlag{
			Name:   "shell",
			Value:  DefaultShell(),
			Usage:  "The shell commamnd used to interpret build commands, e.g /bin/bash -e -c",
			EnvVar: "BUILDKITE_SHELL",
		},
		cli.StringSliceFlag{
			Name:   "tags",
			Value:  &cli.StringSlice{},
			Usage:  "A comma-separated list of tags for the agent (e.g. \"linux\" or \"mac,xcode=8\")",
			EnvVar: "BUILDKITE_AGENT_TAGS",
		},
		cli.BoolFlag{
			Name:   "tags-from-host",
			Usage:  "Include tags from the host (hostname, machine-id, os)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_HOST",
		},
		cli.BoolFlag{
			Name:   "tags-from-ec2",
			Usage:  "Include the host's EC2 meta-data as tags (instance-id, instance-type, and ami-id)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2",
		},
		cli.BoolFlag{
			Name:   "tags-from-ec2-tags",
			Usage:  "Include the host's EC2 tags as tags",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2_TAGS",
		},
		cli.BoolFlag{
			Name:   "tags-from-gcp",
			Usage:  "Include the host's Google Cloud instance meta-data as tags (instance-id, machine-type, preemptible, project-id, region, and zone)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_GCP",
		},
		cli.BoolFlag{
			Name:   "tags-from-gcp-labels",
			Usage:  "Include the host's Google Cloud instance labels as tags",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_GCP_LABELS",
		},
		cli.DurationFlag{
			Name:   "wait-for-ec2-tags-timeout",
			Usage:  "The amount of time to wait for tags from EC2 before proceeding",
			EnvVar: "BUILDKITE_AGENT_WAIT_FOR_EC2_TAGS_TIMEOUT",
			Value:  time.Second * 10,
		},
		cli.DurationFlag{
			Name:   "wait-for-gcp-labels-timeout",
			Usage:  "The amount of time to wait for labels from GCP before proceeding",
			EnvVar: "BUILDKITE_AGENT_WAIT_FOR_GCP_LABELS_TIMEOUT",
			Value:  time.Second * 10,
		},
		cli.StringFlag{
			Name:   "git-clone-flags",
			Value:  "-v",
			Usage:  "Flags to pass to the \"git clone\" command",
			EnvVar: "BUILDKITE_GIT_CLONE_FLAGS",
		},
		cli.StringFlag{
			Name:   "git-clean-flags",
			Value:  "-ffxdq",
			Usage:  "Flags to pass to \"git clean\" command",
			EnvVar: "BUILDKITE_GIT_CLEAN_FLAGS",
		},
		cli.StringFlag{
			Name:   "git-clone-mirror-flags",
			Value:  "-v --mirror",
			Usage:  "Flags to pass to the \"git clone\" command when used for mirroring",
			EnvVar: "BUILDKITE_GIT_CLONE_MIRROR_FLAGS",
		},
		cli.StringFlag{
			Name:   "git-mirrors-path",
			Value:  "",
			Usage:  "Path to where mirrors of git repositories are stored",
			EnvVar: "BUILDKITE_GIT_MIRRORS_PATH",
		},
		cli.IntFlag{
			Name:   "git-mirrors-lock-timeout",
			Value:  300,
			Usage:  "Seconds to lock a git mirror during clone, should exceed your longest checkout",
			EnvVar: "BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
		},
		cli.StringFlag{
			Name:   "bootstrap-script",
			Value:  "",
			Usage:  "The command that is executed for bootstrapping a job, defaults to the bootstrap sub-command of this binary",
			EnvVar: "BUILDKITE_BOOTSTRAP_SCRIPT_PATH",
		},
		cli.StringFlag{
			Name:   "build-path",
			Value:  "",
			Usage:  "Path to where the builds will run from",
			EnvVar: "BUILDKITE_BUILD_PATH",
		},
		cli.StringFlag{
			Name:   "hooks-path",
			Value:  "",
			Usage:  "Directory where the hook scripts are found",
			EnvVar: "BUILDKITE_HOOKS_PATH",
		},
		cli.StringFlag{
			Name:   "plugins-path",
			Value:  "",
			Usage:  "Directory where the plugins are saved to",
			EnvVar: "BUILDKITE_PLUGINS_PATH",
		},
		cli.BoolFlag{
			Name:   "timestamp-lines",
			Usage:  "Prepend timestamps on each line of output.",
			EnvVar: "BUILDKITE_TIMESTAMP_LINES",
		},
		cli.BoolFlag{
			Name:   "no-pty",
			Usage:  "Do not run jobs within a pseudo terminal",
			EnvVar: "BUILDKITE_NO_PTY",
		},
		cli.BoolFlag{
			Name:   "no-ssh-keyscan",
			Usage:  "Don't automatically run ssh-keyscan before checkout",
			EnvVar: "BUILDKITE_NO_SSH_KEYSCAN",
		},
		cli.BoolFlag{
			Name:   "no-command-eval",
			Usage:  "Don't allow this agent to run arbitrary console commands, including plugins",
			EnvVar: "BUILDKITE_NO_COMMAND_EVAL",
		},
		cli.BoolFlag{
			Name:   "no-plugins",
			Usage:  "Don't allow this agent to load plugins",
			EnvVar: "BUILDKITE_NO_PLUGINS",
		},
		cli.BoolTFlag{
			Name:   "no-plugin-validation",
			Usage:  "Don't validate plugin configuration and requirements",
			EnvVar: "BUILDKITE_NO_PLUGIN_VALIDATION",
		},
		cli.BoolFlag{
			Name:   "no-local-hooks",
			Usage:  "Don't allow local hooks to be run from checked out repositories",
			EnvVar: "BUILDKITE_NO_LOCAL_HOOKS",
		},
		cli.BoolFlag{
			Name:   "no-git-submodules",
			Usage:  "Don't automatically checkout git submodules",
			EnvVar: "BUILDKITE_NO_GIT_SUBMODULES,BUILDKITE_DISABLE_GIT_SUBMODULES",
		},
		cli.BoolFlag{
			Name:   "metrics-datadog",
			Usage:  "Send metrics to DogStatsD for Datadog",
			EnvVar: "BUILDKITE_METRICS_DATADOG",
		},
		cli.StringFlag{
			Name:   "metrics-datadog-host",
			Usage:  "The dogstatsd instance to send metrics to via udp",
			EnvVar: "BUILDKITE_METRICS_DATADOG_HOST",
			Value:  "127.0.0.1:8125",
		},
		cli.StringFlag{
			Name:   "log-format",
			Usage:  "The format to use for the logger output",
			EnvVar: "BUILDKITE_LOG_FORMAT",
			Value:  "text",
		},
		cli.IntFlag{
			Name:   "spawn",
			Usage:  "The number of agents to spawn in parallel",
			Value:  1,
			EnvVar: "BUILDKITE_AGENT_SPAWN",
		},

		// API Flags
		AgentRegisterTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		ExperimentsFlag,
		NoColorFlag,
		DebugFlag,

		// Deprecated flags which will be removed in v4
		cli.StringSliceFlag{
			Name:   "meta-data",
			Value:  &cli.StringSlice{},
			Hidden: true,
			EnvVar: "BUILDKITE_AGENT_META_DATA",
		},
		cli.BoolFlag{
			Name:   "meta-data-ec2",
			Hidden: true,
			EnvVar: "BUILDKITE_AGENT_META_DATA_EC2",
		},
		cli.BoolFlag{
			Name:   "meta-data-ec2-tags",
			Hidden: true,
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2_TAGS",
		},
		cli.BoolFlag{
			Name:   "meta-data-gcp",
			Hidden: true,
			EnvVar: "BUILDKITE_AGENT_META_DATA_GCP",
		},
		cli.BoolFlag{
			Name:   "no-automatic-ssh-fingerprint-verification",
			Hidden: true,
			EnvVar: "BUILDKITE_NO_AUTOMATIC_SSH_FINGERPRINT_VERIFICATION",
		},
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := AgentStartConfig{}

		// Setup the config loader. You'll see that we also path paths to
		// potential config files. The loader will use the first one it finds.
		loader := cliconfig.Loader{
			CLI:                    c,
			Config:                 &cfg,
			DefaultConfigFilePaths: DefaultConfigFilePaths(),
		}

		// Load the configuration
		warnings, err := loader.Load()
		if err != nil {
			fmt.Printf("%s", err)
			os.Exit(1)
		}

		l := CreateLogger(cfg)

		// Show warnings now we have a logger
		for _, warning := range warnings {
			l.Warn("%s", warning)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(l, cfg)

		// Remove any config env from the environment to prevent them propagating to bootstrap
		UnsetConfigFromEnvironment(c)

		// Check if git-mirrors are enabled
		if experiments.IsEnabled(`git-mirrors`) {
			if cfg.GitMirrorsPath == `` {
				l.Fatal("Must provide a git-mirrors-path in your configuration for git-mirrors experiment")
			}
		}

		// Force some settings if on Windows (these aren't supported yet)
		if runtime.GOOS == "windows" {
			cfg.NoPTY = true
		}

		// Set a useful default for the bootstrap script
		if cfg.BootstrapScript == "" {
			cfg.BootstrapScript = fmt.Sprintf("%s bootstrap", shellwords.Quote(os.Args[0]))
		}

		// Show a warning if plugins are enabled by no-command-eval or no-local-hooks is set
		if c.IsSet("no-plugins") && cfg.NoPlugins == false {
			msg := `Plugins have been specifically enabled, despite %s being enabled. ` +
				`Plugins can execute arbitrary hooks and commands, make sure you are ` +
				`whitelisting your plugins in ` +
				`your environment hook.`

			switch {
			case cfg.NoCommandEval:
				l.Warn(msg, `no-command-eval`)
			case cfg.NoLocalHooks:
				l.Warn(msg, `no-local-hooks`)
			}
		}

		// Turning off command eval or local hooks will also turn off plugins unless
		// `--no-plugins=false` is provided specifically
		if (cfg.NoCommandEval || cfg.NoLocalHooks) && !c.IsSet("no-plugins") {
			cfg.NoPlugins = true
		}

		// Guess the shell if none is provided
		if cfg.Shell == "" {
			cfg.Shell = DefaultShell()
		}

		// Make sure the DisconnectAfterJobTimeout value is correct
		if cfg.DisconnectAfterJob && cfg.DisconnectAfterJobTimeout < 120 {
			l.Fatal("The timeout for `disconnect-after-job` must be at least 120 seconds")
		}

		var ec2TagTimeout time.Duration
		if t := cfg.WaitForEC2TagsTimeout; t != "" {
			var err error
			ec2TagTimeout, err = time.ParseDuration(t)
			if err != nil {
				l.Fatal("Failed to parse ec2 tag timeout: %v", err)
			}
		}

		var gcpLabelsTimeout time.Duration
		if t := cfg.WaitForGCPLabelsTimeout; t != "" {
			var err error
			gcpLabelsTimeout, err = time.ParseDuration(t)
			if err != nil {
				l.Fatal("Failed to parse gcp labels timeout: %v", err)
			}
		}

		mc := metrics.NewCollector(l, metrics.CollectorConfig{
			Datadog:     cfg.MetricsDatadog,
			DatadogHost: cfg.MetricsDatadogHost,
		})

		// AgentConfiguration is the runtime configuration for an agent
		agentConf := agent.AgentConfiguration{
			BootstrapScript:            cfg.BootstrapScript,
			BuildPath:                  cfg.BuildPath,
			GitMirrorsPath:             cfg.GitMirrorsPath,
			GitMirrorsLockTimeout:      cfg.GitMirrorsLockTimeout,
			HooksPath:                  cfg.HooksPath,
			PluginsPath:                cfg.PluginsPath,
			GitCloneFlags:              cfg.GitCloneFlags,
			GitCloneMirrorFlags:        cfg.GitCloneMirrorFlags,
			GitCleanFlags:              cfg.GitCleanFlags,
			GitSubmodules:              !cfg.NoGitSubmodules,
			SSHKeyscan:                 !cfg.NoSSHKeyscan,
			CommandEval:                !cfg.NoCommandEval,
			PluginsEnabled:             !cfg.NoPlugins,
			PluginValidation:           !cfg.NoPluginValidation,
			LocalHooksEnabled:          !cfg.NoLocalHooks,
			RunInPty:                   !cfg.NoPTY,
			TimestampLines:             cfg.TimestampLines,
			DisconnectAfterJob:         cfg.DisconnectAfterJob,
			DisconnectAfterJobTimeout:  cfg.DisconnectAfterJobTimeout,
			DisconnectAfterIdleTimeout: cfg.DisconnectAfterIdleTimeout,
			CancelGracePeriod:          cfg.CancelGracePeriod,
			Shell:                      cfg.Shell,
		}

		if loader.File != nil {
			agentConf.ConfigPath = loader.File.Path
		}

		if cfg.LogFormat == `text` {
			welcomeMessage :=
				"\n" +
					"%s  _           _ _     _ _    _ _                                _\n" +
					" | |         (_) |   | | |  (_) |                              | |\n" +
					" | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_\n" +
					" | '_ \\| | | | | |/ _` | |/ / | __/ _ \\  / _` |/ _` |/ _ \\ '_ \\| __|\n" +
					" | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_\n" +
					" |_.__/ \\__,_|_|_|\\__,_|_|\\_\\_|\\__\\___|  \\__,_|\\__, |\\___|_| |_|\\__|\n" +
					"                                                __/ |\n" +
					" http://buildkite.com/agent                    |___/\n%s\n"

			if !cfg.NoColor {
				fmt.Fprintf(os.Stderr, welcomeMessage, "\x1b[38;5;48m", "\x1b[0m")
			} else {
				fmt.Fprintf(os.Stderr, welcomeMessage, "", "")
			}
		}

		l.Notice("Starting buildkite-agent v%s with PID: %s", agent.Version(), fmt.Sprintf("%d", os.Getpid()))
		l.Notice("The agent source code can be found here: https://github.com/buildkite/agent")
		l.Notice("For questions and support, email us at: hello@buildkite.com")

		if agentConf.ConfigPath != "" {
			l.WithFields(logger.StringField(`path`, agentConf.ConfigPath)).Info("Configuration loaded")
		}

		l.Debug("Bootstrap command: %s", agentConf.BootstrapScript)
		l.Debug("Build path: %s", agentConf.BuildPath)
		l.Debug("Hooks directory: %s", agentConf.HooksPath)
		l.Debug("Plugins directory: %s", agentConf.PluginsPath)

		if !agentConf.SSHKeyscan {
			l.Info("Automatic ssh-keyscan has been disabled")
		}

		if !agentConf.CommandEval {
			l.Info("Evaluating console commands has been disabled")
		}

		if !agentConf.PluginsEnabled {
			l.Info("Plugins have been disabled")
		}

		if !agentConf.RunInPty {
			l.Info("Running builds within a pseudoterminal (PTY) has been disabled")
		}

		if agentConf.DisconnectAfterJob {
			l.Info("Agent will disconnect after a job run has completed with a timeout of %d seconds",
				agentConf.DisconnectAfterJobTimeout)
		}

		apiClientConf := loadAPIClientConfig(cfg, `Token`)

		// Create the API client
		client := agent.NewAPIClient(l, apiClientConf)

		// The registration request for all agents
		registerReq := api.AgentRegisterRequest{
			Name:              cfg.Name,
			Priority:          cfg.Priority,
			ScriptEvalEnabled: !cfg.NoCommandEval,
			Tags: agent.FetchTags(l, agent.FetchTagsConfig{
				Tags:                    cfg.Tags,
				TagsFromEC2:             cfg.TagsFromEC2,
				TagsFromEC2Tags:         cfg.TagsFromEC2Tags,
				TagsFromGCP:             cfg.TagsFromGCP,
				TagsFromGCPLabels:       cfg.TagsFromGCPLabels,
				TagsFromHost:            cfg.TagsFromHost,
				WaitForEC2TagsTimeout:   ec2TagTimeout,
				WaitForGCPLabelsTimeout: gcpLabelsTimeout,
			}),
		}

		// The common configuration for all workers
		workerConf := agent.AgentWorkerConfig{
			AgentConfiguration: agentConf,
			Debug:              cfg.Debug,
			Endpoint:           apiClientConf.Endpoint,
			DisableHTTP2:       apiClientConf.DisableHTTP2,
		}

		var workers []*agent.AgentWorker

		for i := 1; i <= cfg.Spawn; i++ {
			if cfg.Spawn == 1 {
				l.Info("Registering agent with Buildkite...")
			} else {
				l.Info("Registering agent %d of %d with Buildkite...", i, cfg.Spawn)
			}

			// Register the agent with the buildkite API
			ag, err := agent.Register(l, client, registerReq)
			if err != nil {
				l.Fatal("%s", err)
			}

			// Create an agent worker to run the agent
			workers = append(workers,
				agent.NewAgentWorker(
					l.WithFields(logger.StringField(`agent`, ag.Name)), ag, mc, workerConf))
		}

		// Setup the agent pool that spawns agent workers
		pool := agent.NewAgentPool(l, workers)

		// Start the agent pool
		if err := pool.Start(); err != nil {
			l.Fatal("%s", err)
		}
	},
}
