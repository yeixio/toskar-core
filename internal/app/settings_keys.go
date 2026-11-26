package app

import "github.com/yeixio/toskar-core/pkg/contracts"

// settingsPatchKeys are the fields PATCH /settings takes, the same as the web
// app's SettingsPatch. A field it doesn't know answers INVALID_SETTING, so a
// typo, such as the config file's ollama_port for ollama_port_enabled, isn't
// a 200 that changes nothing (#1464).
var settingsPatchKeys = map[string]bool{
	"advanced_mode":              true,
	"ai_news":                    true,
	"assistant_language":         true,
	"assistant_language_mode":    true,
	"automation_digest":          true,
	"automation_digest_zone":     true,
	"check_links":                true,
	"community_ratings":          true,
	"compact_at":                 true,
	"default_execution":          true,
	"default_profile_id":         true,
	"discovery_enabled":          true,
	"download_behavior":          true,
	"huggingface_token":          true,
	"idle_unload_minutes":        true,
	"keep_running_in_background": true,
	"lan_api_enabled":            true,
	"launch_at_login":            true,
	"main_model_ready":           true,
	"memory_enabled":             true,
	"model_lifecycle":            true,
	"model_storage_limit_gb":     true,
	"node_name":                  true,
	"notify_newer_models":        true,
	"notify_peer_offline":        true,
	"notify_task_finish":         true,
	"ollama_port_enabled":        true,
	"problem_reports":            true,
	"quality_results":            true,
	"ratings_prompts":            true,
	"remote_access_address":      true,
	"remote_access_enabled":      true,
	"remote_access_port":         true,
	"remote_access_port_mapping": true,
	"remote_access_relay":        true,
	"remote_access_relay_secret": true,
	"save_chat_history":          true,
	"save_task_history":          true,
	"suggest_models":             true,
	"thinking_sound":             true,
	"tool_file_writes":           true,
	"tool_git":                   true,
	"tool_terminal":              true,
	"ui_locale":                  true,
	"update_check":               true,
	"voice_avatar":               true,
	"voice_hearing":              true,
	"voice_smart_turn":           true,
	"voice_language":             true,
	"web_ui_enabled":             true,
}

// unknownSetting names the first field of patch that isn't a setting.
func unknownSetting(patch map[string]any) error {
	for k := range patch {
		if !settingsPatchKeys[k] {
			return contracts.Errorf("INVALID_SETTING", map[string]any{"setting": k}, "%s isn't a setting", k)
		}
	}
	return nil
}
