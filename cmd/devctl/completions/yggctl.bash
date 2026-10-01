# bash completion for yggctl

_yggctl() {
  local cur prev
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=($(compgen -W "version about paths automations mcp completion" -- "$cur"))
    return
  fi

  case "${COMP_WORDS[1]}" in
    mcp)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "--url" -- "$cur"))
      fi
      ;;
    completion)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur"))
      fi
      ;;
    automations)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=($(compgen -W "list get create update delete run pause resume" -- "$cur"))
        return
      fi
      case "${COMP_WORDS[2]}" in
        create|update)
          case "$prev" in
            --schedule) COMPREPLY=($(compgen -W "once daily weekly interval" -- "$cur")); return ;;
            --notify) COMPREPLY=($(compgen -W "always condition change none" -- "$cur")); return ;;
            --condition-kind) COMPREPLY=($(compgen -W "threshold available significant" -- "$cur")); return ;;
            --condition-op) COMPREPLY=($(compgen -W "below above" -- "$cur")); return ;;
            --weekday) COMPREPLY=($(compgen -W "0 1 2 3 4 5 6" -- "$cur")); return ;;
          esac
          if [[ "$cur" == -* ]]; then
            COMPREPLY=($(compgen -W "--name --prompt --profile --model --schedule --at --every --weekday --zone --tool --notify --condition-kind --condition-op --condition-value --disabled" -- "$cur"))
          fi
          ;;
      esac
      ;;
  esac
}

complete -F _yggctl yggctl
