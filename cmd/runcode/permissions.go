package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gitlab.ouc-online.com.cn/aibase/agentloop/netorigin"
	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
	"gitlab.ouc-online.com.cn/aibase/agentloop/webaccess"
)

// defaultNetworkTool is the tool that network allow/deny rules apply to. WebFetch
// is currently the only network-class tool, so it is the default for the
// host-oriented allow/deny subcommands.
const defaultNetworkTool = "WebFetch"

// permissionsCmd manages the persistent permission rules stored at
// <workspace>/.runcode/permissions.json — the same allow/deny grain the
// interactive "allow for project" prompt writes. Listing and removal cover every
// rule kind (network, mutation, command); adding rules from the CLI targets
// network hosts, the one key kind that is reliably typeable and matches exactly.
func permissionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "permissions",
		Short:        "Inspect and edit persisted permission rules (.runcode/permissions.json)",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPermissionsList(cmd)
		},
	}
	cmd.PersistentFlags().String("cwd", "", "workspace directory (default: current directory; env RUNCODE_CWD)")

	cmd.AddCommand(permissionsListCmd())
	cmd.AddCommand(permissionsDenyCmd())
	cmd.AddCommand(permissionsAllowCmd())
	cmd.AddCommand(permissionsRemoveCmd())
	cmd.AddCommand(permissionsClearCmd())
	return cmd
}

func permissionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List the persisted allow and deny rules",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPermissionsList(cmd)
		},
	}
}

func permissionsDenyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "deny <host|origin>",
		Short:        "Add a denylist rule for a network host (deny always wins over allow)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openPermissionsStore(cmd)
			if err != nil {
				return err
			}
			tool, _ := cmd.Flags().GetString("tool")
			var key string
			var keyErr error
			if strings.Contains(args[0], "://") {
				route, _ := cmd.Flags().GetString("route")
				diagnose, _ := cmd.Flags().GetBool("diagnose")
				key, keyErr = networkOriginRuleKey(tool, args[0], route, diagnose)
			} else {
				if cmd.Flags().Changed("route") || cmd.Flags().Changed("diagnose") {
					return fmt.Errorf("route/diagnose require an explicit HTTP(S) origin")
				}
				key, keyErr = networkRuleKey(tool, args[0])
			}
			if keyErr != nil {
				return keyErr
			}
			changed, err := store.DenyPersistent(key)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if changed {
				_, _ = fmt.Fprintf(out, "denied %s for %s\n", args[0], tool)
			} else {
				_, _ = fmt.Fprintf(out, "already denied: %s for %s\n", args[0], tool)
			}
			return nil
		},
	}
	cmd.Flags().String("tool", defaultNetworkTool, "tool the rule applies to")
	cmd.Flags().String("route", "auto", "origin-only rule route: auto, configured, or loopback proxy URL")
	cmd.Flags().Bool("diagnose", false, "origin-only rule applies to diagnostics")
	return cmd
}

func permissionsAllowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "allow <origin>",
		Short:        "Allow one explicit HTTP(S) origin and route (legacy host-only allows are not reused)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openPermissionsStore(cmd)
			if err != nil {
				return err
			}
			tool, _ := cmd.Flags().GetString("tool")
			route, _ := cmd.Flags().GetString("route")
			diagnose, _ := cmd.Flags().GetBool("diagnose")
			key, err := networkOriginRuleKey(tool, args[0], route, diagnose)
			if err != nil {
				return err
			}
			origin, _ := netorigin.Parse(args[0])
			parsed, _ := url.Parse(origin)
			host := parsed.Hostname()
			if store.Denied(key) || store.Denied(permissions.NetworkSessionKey(tool, host)) {
				return fmt.Errorf("%s is on the denylist; remove the deny rule first (deny wins over allow)", args[0])
			}
			if err := store.RememberPersistent(key); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "allowed %s for %s\n", args[0], tool)
			return nil
		},
	}
	cmd.Flags().String("tool", defaultNetworkTool, "tool the rule applies to")
	cmd.Flags().String("route", "auto", "WebFetch route: auto, configured, or explicit loopback proxy URL")
	cmd.Flags().Bool("diagnose", false, "allow only origin diagnostics, not fetching content")
	return cmd
}

func permissionsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "remove <number>",
		Short:        "Remove a rule by its number from `permissions list`",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openPermissionsStore(cmd)
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(strings.TrimSpace(args[0]))
			if err != nil || n < 1 {
				return fmt.Errorf("invalid rule number %q (run `runcode permissions list`)", args[0])
			}
			rules := orderedRules(store)
			if n > len(rules) {
				return fmt.Errorf("no rule numbered %d (there are %d)", n, len(rules))
			}
			entry := rules[n-1]
			removed, err := store.Forget(entry.rule.Key)
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("rule %d was already gone", n)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "removed [%d] %s\n", n, formatRule(entry))
			return nil
		},
	}
}

func permissionsClearCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "clear",
		Short:        "Remove all persisted rules (or only allow/deny with the flags)",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openPermissionsStore(cmd)
			if err != nil {
				return err
			}
			allowOnly, _ := cmd.Flags().GetBool("allow")
			denyOnly, _ := cmd.Flags().GetBool("deny")
			// With neither flag, clear both; with one, clear only that list.
			clearAllow := allowOnly || !denyOnly
			clearDeny := denyOnly || !allowOnly
			removed, err := store.ClearPersistent(clearAllow, clearDeny)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cleared %d rule(s)\n", removed)
			return nil
		},
	}
	cmd.Flags().Bool("allow", false, "clear only the allow list")
	cmd.Flags().Bool("deny", false, "clear only the deny list")
	return cmd
}

// openPermissionsStore resolves the workspace and opens its permissions file. A
// corrupt file is surfaced as an error (never silently dropped) so deny rules are
// not lost without notice.
func openPermissionsStore(cmd *cobra.Command) (*permissions.FileAllowStore, error) {
	cwd, err := cwdConfig(cmd)
	if err != nil {
		return nil, err
	}
	return permissions.OpenFileAllowStore(cwd)
}

func networkRuleKey(tool, host string) (string, error) {
	tool = strings.TrimSpace(tool)
	host = strings.TrimSpace(strings.ToLower(host))
	if tool == "" {
		return "", fmt.Errorf("empty tool")
	}
	if host == "" {
		return "", fmt.Errorf("empty host")
	}
	parsed, err := url.Parse("https://" + host)
	if err != nil || parsed.Hostname() == "" || parsed.Port() != "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("use a bare host, or an explicit HTTP(S) origin for a port-specific rule")
	}
	host = parsed.Hostname()
	key := permissions.NetworkSessionKey(tool, host)
	if key == "" {
		return "", fmt.Errorf("invalid host %q", host)
	}
	return key, nil
}

func networkOriginRuleKey(name, raw, route string, diagnose bool) (string, error) {
	name = strings.TrimSpace(name)
	parsed, parseErr := url.Parse(raw)
	if parseErr != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("origin must not contain credentials, a path, query or fragment")
	}
	origin, err := netorigin.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("explicit HTTP(S) origin required, for example https://example.com: %w", err)
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("empty tool")
	}
	if name == "WebSearch" {
		return "", fmt.Errorf("approve WebSearch in the prompt so its configured destination and route are bound")
	}
	route, err = webaccess.ParseRoute(route)
	if err != nil {
		return "", err
	}
	return permissions.NetworkOriginKey(name, origin, route, diagnose), nil
}

// ruleEntry pairs a parsed rule with its list (allow/deny) for display.
type ruleEntry struct {
	list string // "allow" or "deny"
	rule permissions.Rule
}

// orderedRules returns deny rules first, then allow rules, each already sorted by
// the store. The order is stable, so `permissions list` numbering and
// `permissions remove <n>` agree.
func orderedRules(store *permissions.FileAllowStore) []ruleEntry {
	var entries []ruleEntry
	for _, key := range store.Denies() {
		entries = append(entries, ruleEntry{list: "deny", rule: permissions.ParseRule(key)})
	}
	for _, key := range store.Allows() {
		entries = append(entries, ruleEntry{list: "allow", rule: permissions.ParseRule(key)})
	}
	return entries
}

func runPermissionsList(cmd *cobra.Command) error {
	store, err := openPermissionsStore(cmd)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	entries := orderedRules(store)
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(out, "No persisted permission rules.")
		_, _ = fmt.Fprintln(out, "Add one with `runcode permissions deny <host>` or grant \"allow for project\" in the TUI.")
		return nil
	}
	for i, entry := range entries {
		_, _ = fmt.Fprintf(out, "[%d] %s\n", i+1, formatRule(entry))
	}
	return nil
}

// formatRule renders a rule as "<list> <scope> <tool> <target>", e.g.
// "deny  network WebFetch example.com". A key that did not parse falls back to
// showing the raw key so nothing is hidden.
func formatRule(entry ruleEntry) string {
	r := entry.rule
	scope := r.Scope
	if scope == "" {
		scope = "?"
	}
	parts := []string{fmt.Sprintf("%-5s", entry.list), scope}
	if r.Tool != "" {
		parts = append(parts, r.Tool)
	}
	if r.Target != "" {
		parts = append(parts, r.Target)
	}
	return strings.Join(parts, " ")
}
