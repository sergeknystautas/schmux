import type { ConfigUpdateRequest } from '../../lib/types';
import type { ConfigFormState } from './useConfigForm';

/**
 * Pure function that builds a ConfigUpdateRequest from the current form state.
 * Used by the auto-save system to construct the API payload.
 */
export function buildConfigUpdate(state: ConfigFormState): ConfigUpdateRequest {
  const runTargets = state.commandTargets.map((t) => ({ name: t.name, command: t.command }));

  return {
    workspace_path: state.workspacePath,
    source_code_management: state.sourceCodeManagement,
    recycle_workspaces: state.recycleWorkspaces,
    repos: state.repos,
    run_targets: runTargets,
    quick_launch: state.quickLaunch.map((q) => ({
      ...q,
      prompt: q.prompt ?? undefined,
    })),
    external_diff_commands: state.externalDiffCommands,
    external_diff_cleanup_after_ms: Math.max(
      60000,
      Math.round(state.externalDiffCleanupMinutes * 60000)
    ),
    pastebin: state.pastebin,
    nudgenik: {
      target: state.nudgenikTarget || '',
      viewed_buffer_ms: state.viewedBuffer,
      seen_interval_ms: state.nudgenikSeenInterval,
    },
    branch_suggest: { target: state.branchSuggestTarget || '' },
    conflict_resolve: { target: state.conflictResolveTarget || '' },
    pr_review: { target: state.prReviewTarget || '' },
    commit_message: { target: state.commitMessageTarget || '' },
    sessions: {
      dashboard_poll_interval_ms: state.dashboardPollInterval,
      git_status_poll_interval_ms: state.gitStatusPollInterval,
      git_clone_timeout_ms: state.gitCloneTimeout,
      git_status_timeout_ms: state.gitStatusTimeout,
    },
    xterm: {
      query_timeout_ms: state.xtermQueryTimeout,
      operation_timeout_ms: state.xtermOperationTimeout,
      use_webgl: state.xtermUseWebGL,
    },
    network: {
      bind_address: state.networkAccess ? '0.0.0.0' : '127.0.0.1',
      public_base_url: state.authPublicBaseURL,
      tls: {
        cert_path: state.authTlsCertPath,
        key_path: state.authTlsKeyPath,
      },
    },
    access_control: {
      enabled: state.authEnabled,
      provider: state.authProvider,
      session_ttl_minutes: state.authSessionTTLMinutes,
    },
    notifications: {
      sound_disabled: state.soundDisabled,
      confirm_before_close: state.confirmBeforeClose,
      suggest_dispose_after_push: state.suggestDisposeAfterPush,
    },
    lore: {
      enabled: state.autolearnEnabled,
      llm_target: state.loreLLMTarget,
      curate_on_dispose: state.loreCurateOnDispose,
      auto_pr: state.loreAutoPR,
      public_rule_mode: state.lorePublicRuleMode,
    },
    subreddit: {
      enabled: state.subredditEnabled,
      target: state.subredditTarget,
      interval: state.subredditInterval,
      checking_range: state.subredditCheckingRange,
      max_posts: state.subredditMaxPosts,
      max_age: state.subredditMaxAge,
      repos: state.subredditRepos,
    },
    repofeed: {
      enabled: state.repofeedEnabled,
      publish_interval_seconds: state.repofeedPublishInterval,
      fetch_interval_seconds: state.repofeedFetchInterval,
      completed_retention_hours: state.repofeedCompletedRetention,
      repos: state.repofeedRepos,
    },
    enabled_models: state.enabledModels,
    comm_styles: state.commStyles,
    remote_access: {
      enabled: state.remoteAccessEnabled,
      timeout_minutes: state.remoteAccessTimeoutMinutes,
      notify: {
        ntfy_topic: state.remoteAccessNtfyTopic,
        command: state.remoteAccessNotifyCommand,
      },
    },
    desync: {
      enabled: state.desyncEnabled,
      target: state.desyncTarget || '',
    },
    floor_manager: {
      enabled: state.fmEnabled,
      target: state.fmTarget || '',
      rotation_threshold: state.fmRotationThreshold,
      debounce_ms: state.fmDebounceMs,
    },
    timelapse: {
      enabled: state.timelapseEnabled,
      retention_days: state.timelapseRetentionDays,
      max_file_size_mb: state.timelapseMaxFileSizeMB,
      max_total_storage_mb: state.timelapseMaxTotalStorageMB,
    },
    io_workspace_telemetry: {
      enabled: state.ioWorkspaceTelemetryEnabled,
      target: state.ioWorkspaceTelemetryTarget || '',
    },
    sapling_commands:
      state.saplingCmdCreateWorkspace ||
      state.saplingCmdRemoveWorkspace ||
      state.saplingCmdCheckRepoBase ||
      state.saplingCmdCreateRepoBase
        ? {
            create_workspace: state.saplingCmdCreateWorkspace || undefined,
            remove_workspace: state.saplingCmdRemoveWorkspace || undefined,
            check_repo_base: state.saplingCmdCheckRepoBase || undefined,
            create_repo_base: state.saplingCmdCreateRepoBase || undefined,
          }
        : undefined,
    tmux_binary: state.tmuxBinary || undefined,
    tmux_socket_name: state.tmuxSocketName || undefined,
    personas_enabled: state.personasEnabled,
    comm_styles_enabled: state.commStylesEnabled,
    backburner_enabled: state.backburnerEnabled,
    local_echo_remote: state.localEchoRemote,
    debug_ui: state.debugUI,
  };
}
