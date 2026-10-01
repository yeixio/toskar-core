# fish completion for yggctl

complete -c yggctl -f

complete -c yggctl -n __fish_use_subcommand -a version -d 'Print the version, license, and source URL'
complete -c yggctl -n __fish_use_subcommand -a about -d 'Print the version, license, and source URL'
complete -c yggctl -n __fish_use_subcommand -a paths -d 'Print the data, model, runtime, log, and database directories'
complete -c yggctl -n __fish_use_subcommand -a automations -d 'Manage scheduled automations on the daemon'
complete -c yggctl -n __fish_use_subcommand -a mcp -d 'Connect an app such as Claude Desktop to Yggdrasil over MCP'
complete -c yggctl -n __fish_use_subcommand -a completion -d 'Print a shell completion script'

complete -c yggctl -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
complete -c yggctl -n '__fish_seen_subcommand_from mcp' -l url -r -d 'Yggdrasil address'

set -l ygg_automation_cmds list get create update delete run pause resume
complete -c yggctl -n "__fish_seen_subcommand_from automations; and not __fish_seen_subcommand_from $ygg_automation_cmds" -a "$ygg_automation_cmds"

set -l ygg_edit '__fish_seen_subcommand_from automations; and __fish_seen_subcommand_from create update'
complete -c yggctl -n $ygg_edit -l name -r -d 'Automation name'
complete -c yggctl -n $ygg_edit -l prompt -r -d 'Prompt to run'
complete -c yggctl -n $ygg_edit -l profile -r -d 'Profile id'
complete -c yggctl -n $ygg_edit -l model -r -d 'Installed model id'
complete -c yggctl -n $ygg_edit -l schedule -x -a 'once daily weekly interval' -d 'Schedule kind'
complete -c yggctl -n $ygg_edit -l at -r -d 'HH:MM or RFC3339'
complete -c yggctl -n $ygg_edit -l every -r -d 'Interval duration, such as 6h'
complete -c yggctl -n $ygg_edit -l weekday -x -a '0 1 2 3 4 5 6' -d '0-6, Sunday is 0'
complete -c yggctl -n $ygg_edit -l zone -r -d 'IANA time zone'
complete -c yggctl -n $ygg_edit -l tool -r -d 'Tool id allowed for this automation, repeatable'
complete -c yggctl -n $ygg_edit -l notify -x -a 'always condition change none' -d 'Notification mode'
complete -c yggctl -n $ygg_edit -l condition-kind -x -a 'threshold available significant' -d 'Condition kind'
complete -c yggctl -n $ygg_edit -l condition-op -x -a 'below above' -d 'Condition operator'
complete -c yggctl -n $ygg_edit -l condition-value -r -d 'Threshold value'
complete -c yggctl -n $ygg_edit -l disabled -d 'Create the automation paused'
