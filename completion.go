package main

import (
	"fmt"
	"os"
	"strings"
)

func completion(args []string) error {
	shell := ""
	if len(args) > 1 {
		shell = args[1]
	}
	script, err := completionScript(shell)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "Usage: nextdns completion <bash|zsh|fish>")
		os.Exit(1)
	}
	fmt.Print(script)
	return nil
}

func completionScript(shell string) (string, error) {
	var names []string
	for _, c := range commands {
		names = append(names, c.name)
	}
	cmds := strings.Join(names, " ")
	switch shell {
	case "bash":
		return fmt.Sprintf(bashCompletion, cmds), nil
	case "zsh":
		return fmt.Sprintf(zshCompletion, cmds), nil
	case "fish":
		return fmt.Sprintf(fishCompletion, cmds), nil
	default:
		return "", fmt.Errorf("unsupported or missing shell: %q", shell)
	}
}

const bashCompletion = `_nextdns() {
    local cur
    cur="${COMP_WORDS[COMP_CWORD]}"
    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "%s" -- "$cur") )
        return 0
    fi
    if [ "${COMP_WORDS[1]}" = "config" ] && [ "$COMP_CWORD" -eq 2 ]; then
        COMPREPLY=( $(compgen -W "list set edit wizard" -- "$cur") )
    fi
}
complete -F _nextdns nextdns
`

const zshCompletion = `#compdef nextdns
_nextdns() {
    if (( CURRENT == 2 )); then
        compadd -- %s
        return
    fi
    if [[ ${words[2]} == config && CURRENT == 3 ]]; then
        compadd -- list set edit wizard
    fi
}
compdef _nextdns nextdns
`

const fishCompletion = `complete -c nextdns -f
complete -c nextdns -n __fish_use_subcommand -a '%s'
complete -c nextdns -n '__fish_seen_subcommand_from config' -a 'list set edit wizard'
`
