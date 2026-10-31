// Command shedlab runs throwaway shed instances in local VMs for development.
//
// Labs are real installs: an Ubuntu VM, shed's installer, Docker, systemd. Only
// the binary comes from this checkout and the ports from the lab's slot.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"charm.land/fang/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/spf13/cobra"

	"github.com/kyledickey/shed/internal/github"
)

func main() {
	if err := fang.Execute(context.Background(), rootCmd(), fang.WithColorSchemeFunc(fangColors), fang.WithVersion("dev"), fang.WithoutManpage()); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "shedlab",
		Short: "Throwaway shed instances in local VMs",
		Long:  "Throwaway shed instances in local VMs. Each lab is a real install, built from this checkout.",
		Example: `# start a lab and open it, already signed in
shedlab up firehose --no-auth

# ship your changes to it
shedlab push`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return listLabs() },
	}
	root.AddCommand(upCmd(), pushCmd(), openCmd(), sshCmd(), logsCmd(), stopCmd(), startCmd(), downCmd(), saveCmd(), authCmd(), doctorCmd())
	return root
}

func upCmd() *cobra.Command {
	var opts provision
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "up [scenario]",
		Short: "Start a new lab",
		Long: "Start a new lab from a scenario. Without saved credentials, or with --no-auth, " +
			"the lab has no GitHub App and `shedlab open` signs you in directly.",
		Example: `shedlab up
shedlab up real
shedlab up empty --no-auth
shedlab up firehose --release v0.2.6
shedlab up --from big-data`,
		Args: cobra.MaximumNArgs(1),
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			all, _ := loadScenarios()
			var names []string
			for _, s := range all {
				names = append(names, s.Name+"\t"+s.Description)
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if problems := missingRequirements(); len(problems) > 0 {
				return fmt.Errorf("missing %s; run `shedlab doctor`", strings.Join(problems, ", "))
			}
			var err error
			switch {
			case opts.from != "":
				opts.scenario, err = snapshotScenario(opts.from)
			case len(args) == 1:
				opts.scenario, err = findScenario(args[0])
			default:
				opts.scenario, err = pickScenario()
			}
			if err != nil {
				return err
			}

			l, steps, err := planUp(ctx, opts)
			if err != nil {
				return err
			}
			lipgloss.Println(heading("Starting " + opts.scenario.Name + " in " + l.Slot.name()))
			lipgloss.Println()
			if err := runSteps(ctx, steps); err != nil {
				if errors.Is(err, context.Canceled) {
					return removeCanceledLab(l)
				}
				return fmt.Errorf("%w\n\nThe failed lab is still there to inspect. Remove it with `shedlab down %s`", err, l.Slot.name())
			}
			lipgloss.Println()
			lipgloss.Println(labCard(l))
			lipgloss.Println()
			lipgloss.Println(hints(
				[2]string{"shedlab push", "ship this checkout to the lab"},
				[2]string{"shedlab open", "open the dashboard, signed in"},
				[2]string{"shedlab logs", "follow shed's log"},
				[2]string{"shedlab down " + l.Slot.name(), "delete the lab"},
			))
			if !noOpen && interactive() {
				return openLab(ctx, l)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.noAuth, "no-auth", false, "skip the GitHub App even if credentials are saved")
	cmd.Flags().StringVar(&opts.release, "release", "", "install this release instead of building the checkout")
	cmd.Flags().StringVar(&opts.from, "from", "", "start from a snapshot made with `shedlab save`")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "don't open the browser")
	cmd.MarkFlagsMutuallyExclusive("release", "from")
	return cmd
}

// removeCanceledLab deletes a lab whose `up` was interrupted, because nothing
// about a half-started lab is worth keeping.
func removeCanceledLab(l lab) error {
	if err := l.vm().kill(context.Background()); err != nil {
		return err
	}
	if err := os.RemoveAll(labDir(l.Slot)); err != nil {
		return err
	}
	lipgloss.Println()
	lipgloss.Println(ui.muted.Render("  Canceled. " + l.Slot.name() + " is gone."))
	return nil
}

func pickScenario() (scenario, error) {
	all, err := loadScenarios()
	if err != nil {
		return scenario{}, err
	}
	var options []huh.Option[string]
	for _, s := range all {
		label := fmt.Sprintf("%-10s %s", s.Name, ui.faint.Render(s.Description))
		options = append(options, huh.NewOption(label, s.Name))
	}
	var name string
	if err := runForm(huh.NewSelect[string]().Title("What should this lab run?").Options(options...).Value(&name)); err != nil {
		return scenario{}, err
	}
	return findScenario(name)
}

func labCard(l lab) string {
	rows := []row{
		{"dashboard", ui.code.Render(l.URL)},
		{"apps", ui.body.Render(fmt.Sprintf("https://<service>-<project>.%s:%d", l.Slot.localHost(), l.Slot.httpsPort()))},
		{"source", ui.body.Render(l.Source)},
		{"auth", ui.body.Render(authLabel(l))},
		{"size", ui.body.Render(fmt.Sprintf("%d vCPU, %d GB", l.CPUs, l.MemoryGB))},
	}
	return card(ui.title.Render(l.Slot.name())+"  "+ui.faint.Render(l.Scenario), rows...)
}

func authLabel(l lab) string {
	if l.Auth == authGitHub {
		return "GitHub App, " + l.Login
	}
	return "none, signed in as " + l.Login
}

func listLabs() error {
	labs, err := loadLabs()
	if err != nil {
		return err
	}
	if len(labs) == 0 {
		lipgloss.Println(heading("No labs yet"))
		lipgloss.Println()
		lipgloss.Println(hints(
			[2]string{"shedlab up", "choose a scenario and start a lab"},
			[2]string{"shedlab auth", "save GitHub credentials for labs to reuse"},
			[2]string{"shedlab doctor", "check that this machine is ready"},
		))
		return nil
	}
	rows := make([][]string, len(labs))
	for i, l := range labs {
		state := ui.faint.Render("stopped")
		if l.vm().running() {
			state = ui.good.Render("running")
		}
		rows[i] = []string{ui.title.Render(l.Slot.name()), ui.body.Render(l.Scenario), state, ui.muted.Render(l.Source), ui.code.Render(l.URL), ui.faint.Render(age(l.Created))}
	}
	t := table.New().
		Headers("lab", "scenario", "state", "source", "dashboard", "age").
		Rows(rows...).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.line)).
		BorderColumn(false).BorderRow(false).BorderLeft(false).BorderRight(false).BorderTop(false).BorderBottom(false).BorderHeader(true).
		StyleFunc(func(r, _ int) lipgloss.Style {
			if r == table.HeaderRow {
				return ui.faint.PaddingRight(3)
			}
			return lipgloss.NewStyle().PaddingRight(3)
		})
	lipgloss.Println(heading(plural(len(labs), "lab")))
	lipgloss.Println()
	lipgloss.Println(lipgloss.NewStyle().PaddingLeft(2).Render(t.Render()))
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	}
	return strconv.Itoa(int(d.Hours()/24)) + "d"
}

// selectLab resolves an optional lab argument. With none it uses the only
// candidate, or asks when there are several.
func selectLab(args []string, candidate func(lab) bool) (lab, error) {
	if len(args) == 1 {
		s, ok := parseSlot(args[0])
		if !ok {
			return lab{}, fmt.Errorf("%q is not a lab; labs are lab1 to lab%d", args[0], slotCount)
		}
		l, err := loadLab(s)
		if errors.Is(err, fs.ErrNotExist) {
			return lab{}, fmt.Errorf("%s doesn't exist; start one with `shedlab up`", s.name())
		}
		return l, err
	}
	labs, err := loadLabs()
	if err != nil {
		return lab{}, err
	}
	var options []huh.Option[slot]
	var only lab
	for _, l := range labs {
		if candidate(l) {
			options = append(options, huh.NewOption(l.Slot.name()+"  "+ui.faint.Render(l.Scenario), l.Slot))
			only = l
		}
	}
	switch len(options) {
	case 0:
		return lab{}, errors.New("no matching lab; see `shedlab`")
	case 1:
		return only, nil
	}
	var s slot
	if err := runForm(huh.NewSelect[slot]().Title("Which lab?").Options(options...).Value(&s)); err != nil {
		return lab{}, err
	}
	return loadLab(s)
}

func isRunning(l lab) bool { return l.vm().running() }
func isStopped(l lab) bool { return !l.vm().running() }
func anyLab(lab) bool      { return true }

func labArgs(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	labs, _ := loadLabs()
	var names []string
	for _, l := range labs {
		names = append(names, l.Slot.name()+"\t"+l.Scenario)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func pushCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "push [lab]",
		Short:             "Build this checkout and restart a lab on it",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: labArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			l, err := selectLab(args, isRunning)
			if err != nil {
				return err
			}
			root, err := repoRoot(ctx)
			if err != nil {
				return err
			}
			var binary string
			lipgloss.Println(heading("Pushing this checkout to " + l.Slot.name()))
			lipgloss.Println()
			err = runSteps(ctx, []step{
				{"Build shed", func(ctx context.Context, p *progress) (err error) {
					binary, err = buildShed(ctx, p, root)
					return err
				}},
				{"Restart " + l.Slot.name(), func(ctx context.Context, _ *progress) error {
					return installBinary(ctx, l, binary)
				}},
			})
			if err != nil {
				return err
			}
			l.Source = "dev"
			return l.save()
		},
	}
}

func openCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "open [lab]",
		Short:             "Open a lab's dashboard, signed in",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: labArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := selectLab(args, isRunning)
			if err != nil {
				return err
			}
			return openLab(cmd.Context(), l)
		},
	}
}

// openLab signs the browser in and opens the dashboard. A lab on the public
// domain uses GitHub sign-in. A local lab gets its session cookie from a
// one-shot server on the same hostname, because cookies ignore the port.
func openLab(ctx context.Context, l lab) error {
	if l.Public {
		if !openBrowser(l.URL) {
			lipgloss.Println(ui.muted.Render("Open ") + ui.code.Render(l.URL))
		}
		return nil
	}
	if err := execGuestSQL(ctx, l, sessionSQL(l, time.Now())); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	done := make(chan struct{}, 1)
	srv := &http.Server{Handler: handoff(l, done), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()

	url := fmt.Sprintf("http://%s:%d/", l.Slot.localHost(), ln.Addr().(*net.TCPAddr).Port)
	if !interactive() || !openBrowser(url) {
		lipgloss.Println(ui.muted.Render("Open ") + ui.code.Render(url) + ui.muted.Render(" within two minutes to sign in"))
	}
	select {
	case <-done:
		time.Sleep(200 * time.Millisecond)
		return nil
	case <-time.After(2 * time.Minute):
		return errors.New("the browser never opened the sign-in link; run `shedlab open` to try again")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// handoff sets the lab's session cookie and redirects to its dashboard.
func handoff(l lab, done chan<- struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: l.Session, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 365 * 24 * 3600})
		http.Redirect(w, r, l.URL+"/", http.StatusFound)
		select {
		case done <- struct{}{}:
		default:
		}
	})
}

func sshCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "ssh [lab] [-- command]",
		Short:             "Open a shell in a lab, or run a command",
		Example:           "shedlab ssh\nshedlab ssh lab2 -- sudo docker ps",
		ValidArgsFunction: labArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var labArg, command []string
			if at := cmd.ArgsLenAtDash(); at >= 0 {
				labArg, command = args[:at], args[at:]
			} else {
				labArg = args
			}
			if len(labArg) > 1 {
				return errors.New("pass one lab; put a command after --")
			}
			l, err := selectLab(labArg, isRunning)
			if err != nil {
				return err
			}
			return execSSH(l, strings.Join(command, " "))
		},
	}
}

func logsCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "logs [lab]",
		Short:             "Follow shed's log in a lab",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: labArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := selectLab(args, isRunning)
			if err != nil {
				return err
			}
			return execSSH(l, "sudo journalctl -fu shed -n 100")
		},
	}
}

// execSSH replaces this process with ssh, so the terminal is the guest's.
func execSSH(l lab, command string) error {
	path, err := exec.LookPath("ssh")
	if err != nil {
		return err
	}
	args := l.vm().sshArgs(command)
	if command == "" || strings.HasPrefix(command, "sudo journalctl") {
		args = append([]string{"-t"}, args...)
	}
	return syscall.Exec(path, append([]string{"ssh"}, args...), os.Environ())
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "stop [lab]",
		Short:             "Shut a lab down and free its CPU and memory",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: labArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := selectLab(args, isRunning)
			if err != nil {
				return err
			}
			return runSteps(cmd.Context(), []step{{"Stop " + l.Slot.name(), func(ctx context.Context, _ *progress) error {
				return l.vm().stop(ctx)
			}}})
		},
	}
}

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "start [lab]",
		Short:             "Start a stopped lab",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: labArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := selectLab(args, isStopped)
			if err != nil {
				return err
			}
			return runSteps(cmd.Context(), []step{{"Start " + l.Slot.name(), func(ctx context.Context, _ *progress) error {
				v := l.vm()
				if err := v.start(ctx); err != nil {
					return err
				}
				if err := v.waitSSH(ctx); err != nil {
					return err
				}
				if l.Public {
					if err := ensureTunnel(); err != nil {
						return err
					}
				}
				return waitHealthy(ctx, l)
			}}})
		},
	}
}

func downCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:               "down [lab]",
		Short:             "Delete a lab and its disk",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: labArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var targets []lab
			if all {
				var err error
				if targets, err = loadLabs(); err != nil {
					return err
				}
			} else {
				l, err := selectLab(args, anyLab)
				if err != nil {
					return err
				}
				targets = []lab{l}
			}
			var steps []step
			for _, l := range targets {
				steps = append(steps, step{"Delete " + l.Slot.name(), func(ctx context.Context, _ *progress) error {
					if err := l.vm().kill(ctx); err != nil {
						return err
					}
					return os.RemoveAll(labDir(l.Slot))
				}})
			}
			if err := runSteps(cmd.Context(), steps); err != nil {
				return err
			}
			return stopIdleTunnel()
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "delete every lab")
	return cmd
}

// stopIdleTunnel stops the tunnel once no lab needs it.
func stopIdleTunnel() error {
	pid := tunnelPID()
	if pid == 0 {
		return nil
	}
	labs, err := loadLabs()
	if err != nil {
		return err
	}
	for _, l := range labs {
		if l.Public && l.vm().running() {
			return nil
		}
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

var snapshotName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func saveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "save <name> [lab]",
		Short:   "Snapshot a lab so that new labs can start from it",
		Long:    "Snapshot a lab's disk, data included. The lab restarts afterwards. Start from it with `shedlab up --from <name>`.",
		Example: "shedlab save big-data lab1\nshedlab up --from big-data",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !snapshotName.MatchString(name) {
				return errors.New("snapshot names use lowercase letters, digits, and dashes")
			}
			l, err := selectLab(args[1:], anyLab)
			if err != nil {
				return err
			}
			wasRunning := l.vm().running()
			return runSteps(cmd.Context(), []step{
				{"Stop " + l.Slot.name(), func(ctx context.Context, _ *progress) error { return l.vm().stop(ctx) }},
				{"Save " + name, func(ctx context.Context, _ *progress) error { return saveSnapshot(ctx, l, name) }},
				{"Restart " + l.Slot.name(), func(ctx context.Context, _ *progress) error {
					if !wasRunning {
						return nil
					}
					if err := l.vm().start(ctx); err != nil {
						return err
					}
					return waitHealthy(ctx, l)
				}},
			})
		},
	}
}

func saveSnapshot(ctx context.Context, l lab, name string) error {
	if err := disableCoW(snapshotDir()); err != nil {
		return err
	}
	tmp := snapshotDisk(name) + ".tmp"
	defer os.Remove(tmp)
	if _, err := run(ctx, "qemu-img", "convert", "-O", "qcow2", l.vm().disk(), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, snapshotDisk(name)); err != nil {
		return err
	}
	meta := fmt.Sprintf("description = %q\ncpus = %d\nmemory_gb = %d\ndisk_gb = %d\n", "Snapshot of "+l.Scenario, l.CPUs, l.MemoryGB, snapshotDiskGB)
	return writeFileAtomic(filepath.Join(snapshotDir(), name+".toml"), []byte(meta), 0o644)
}

// snapshotDiskGB is large enough for any scenario: qcow2 only allocates what
// the guest writes.
const snapshotDiskGB = 200

func snapshotScenario(name string) (scenario, error) {
	if !snapshotName.MatchString(name) {
		return scenario{}, fmt.Errorf("no snapshot %q", name)
	}
	if _, err := os.Stat(snapshotDisk(name)); err != nil {
		return scenario{}, fmt.Errorf("no snapshot %q; make one with `shedlab save`", name)
	}
	return parseScenarioFile(filepath.Join(snapshotDir(), name+".toml"), name)
}

func authCmd() *cobra.Command {
	var reset bool
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Save GitHub credentials for labs to reuse",
		Long: "Create one GitHub App for all labs and save it, so that new labs never ask for an ID or secret. " +
			"Optionally make labs reachable at lab1." + publicDomain + " and so on, for real sign-in and webhooks.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuth(cmd.Context(), reset)
		},
	}
	cmd.Flags().BoolVar(&reset, "reset", false, "create a new GitHub App instead of keeping the saved one")
	return cmd
}

func runAuth(ctx context.Context, reset bool) error {
	creds, err := loadCredentials()
	if err != nil {
		return err
	}
	if reset {
		creds.App = nil
	}
	lipgloss.Println(heading("Saving credentials for labs"))
	lipgloss.Println()

	public := creds.Tunnel
	if creds.Login == "" {
		creds.Login = defaultGitHubLogin(ctx)
	}
	err = runForm(
		huh.NewInput().Title("GitHub username").Description("Allowed to sign in to labs.").Value(&creds.Login).Validate(checkLogin),
		huh.NewConfirm().Title("Reach labs at lab1."+publicDomain+"?").
			Description("Needed for GitHub webhooks. Uses a Cloudflare tunnel; otherwise labs stay on localhost.").
			Value(&public),
	)
	if err != nil {
		return err
	}

	steps := []step{{"Check GitHub user", func(ctx context.Context, _ *progress) (err error) {
		creds.GitHubID, err = githubUser(ctx, creds.Login)
		return err
	}}}
	if creds.App == nil {
		steps = append(steps, step{"Create GitHub App", func(ctx context.Context, p *progress) error {
			app, err := createGitHubApp(ctx, p, creds.Login)
			if err != nil {
				return err
			}
			creds.setApp(app)
			return creds.save()
		}})
	}
	if public {
		steps = append(steps, step{"Set up tunnel", func(ctx context.Context, p *progress) error {
			if _, err := exec.LookPath("cloudflared"); err != nil {
				return errors.New("cloudflared isn't installed")
			}
			return setupTunnel(ctx, p)
		}})
	}
	if err := runSteps(ctx, steps); err != nil {
		return err
	}
	creds.Tunnel = public
	if err := creds.save(); err != nil {
		return err
	}
	where := "localhost"
	if public {
		where = "lab1." + publicDomain + " to lab" + strconv.Itoa(slotCount) + "." + publicDomain
	}
	lipgloss.Println()
	lipgloss.Println(card(ui.title.Render("Credentials saved"),
		row{"user", ui.body.Render(creds.Login)},
		row{"app", ui.code.Render("github.com/apps/" + creds.App.Slug)},
		row{"labs", ui.body.Render(where)},
		row{"file", ui.faint.Render(credentialsPath())},
	))
	lipgloss.Println()
	lipgloss.Println(hints([2]string{"shedlab up", "start a lab with these credentials"}))
	return nil
}

func defaultGitHubLogin(ctx context.Context) string {
	out, err := run(ctx, "gh", "api", "user", "-q", ".login")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// setApp stores app in the credentials.
func (c *credentials) setApp(app github.App) {
	c.App = &savedApp{
		ID: app.ID, Slug: app.Slug, ClientID: app.ClientID, ClientSecret: app.ClientSecret,
		WebhookSecret: app.WebhookSecret, PrivateKey: string(app.PrivateKey),
	}
}

// requirement is something `up` or `auth` needs from this machine.
type requirement struct {
	name     string
	optional bool // Only some features need it.
	check    func() error
	fix      string
}

func onPath(tool string) func() error {
	return func() error {
		_, err := exec.LookPath(tool)
		return err
	}
}

var requirements = []requirement{
	{name: "KVM", check: func() error {
		f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
		if err != nil {
			return err
		}
		return f.Close()
	}, fix: "enable virtualization in the BIOS and join the kvm group"},
	{name: "qemu-system-x86_64", check: onPath("qemu-system-x86_64"), fix: "sudo pacman -S qemu-base"},
	{name: "qemu-img", check: onPath("qemu-img"), fix: "sudo pacman -S qemu-base"},
	{name: "ssh", check: onPath("ssh"), fix: "sudo pacman -S openssh"},
	{name: "go", check: onPath("go"), fix: "sudo pacman -S go"},
	{name: "make", check: onPath("make"), fix: "sudo pacman -S make"},
	{name: "bun", optional: true, check: onPath("bun"), fix: "only for rebuilding the dashboard: https://bun.sh"},
	{name: "cloudflared", optional: true, check: onPath("cloudflared"), fix: "only for labs on " + publicDomain + ": sudo pacman -S cloudflared"},
}

func missingRequirements() []string {
	var missing []string
	for _, r := range requirements {
		if !r.optional && r.check() != nil {
			missing = append(missing, r.name)
		}
	}
	return missing
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that this machine can run labs",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			lipgloss.Println(heading("Checking this machine"))
			lipgloss.Println()
			var lines []row
			failed := false
			for _, r := range requirements {
				err := r.check()
				switch {
				case err == nil:
					lines = append(lines, row{r.name, ui.good.Render("ok")})
				case r.optional:
					lines = append(lines, row{r.name, ui.faint.Render("missing, " + r.fix)})
				default:
					failed = true
					lines = append(lines, row{r.name, ui.bad.Render("missing") + "  " + ui.body.Render(r.fix)})
				}
			}
			for _, r := range lines {
				lipgloss.Println("  " + ui.muted.Width(22).Render(r.key) + r.value)
			}
			if failed {
				return errors.New("fix the items above, then run `shedlab up`")
			}
			return nil
		},
	}
}
