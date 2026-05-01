# Test Suite

Extracted from `agentctx/resume.md` for reference.

**1918 tests** across 41 test packages + **1 integration test** (31 subtests) + **21 vault-accessor integration tests**. All passing. (Direction-C iter 168 retired the wrap-bundle / QC / dispatch test inventories — see DESIGN #92; the stamp-file iter anchor adds `tools_stamp_iter_test.go` and reshapes `tools_describe_iter_state_test.go` — see DESIGN #93. The Phase 1+2 ship of `feat/hook-shape-validation` (DESIGN #96, iter 180) adds `internal/hook/validate_test.go` — the `ValidateHooks` table-driven validator covering the iter-178 regression fixture, all 5 `hookCommand` `anyOf` variants, and strict `additionalProperties: false` enforcement — plus pre-write refusal/coexistence cases across all four `Install*` paths in `internal/hook/setup_test.go`.)

> **Note (iter 121):** The detailed per-file table below is out of date relative to the
> headline counts above — accumulated drift across many iterations. Treat the table
> as indicative rather than authoritative; use `go test -v ./... 2>&1 | grep -c '^=== RUN'`
> for the current total and `go test -v ./internal/<package>` to enumerate tests in any
> specific package.

Run unit tests: `make test` (or `go test -short ./...`)
Run integration: `make integration` (or `go test -run TestIntegration -timeout 60s ./test/`)

## Unit Tests

| File | Tests | Coverage |
|------|-------|----------|
| `session/capture_test.go` | 7 | `CaptureFromParsed_Basic`, `CaptureFromParsed_ZedSource`, `CaptureFromParsed_TrivialSkip`, `CaptureFromParsed_Dedup`, `CaptureFromParsed_ZedFallbackSummary`, `CaptureFromParsed_WithNarrativeAndDialogue`, `CaptureFromParsed_Idempotent` |
| `session/detect_test.go` | 8 | `DetectProject` (5 variants: baseline, git remote SSH/HTTPS, no remote, not git repo), `detectDomain`, `TitleFromFirstMessage`, `repoNameFromURL` |
| `transcript/parser_test.go` | 7 | `Parse` (full round-trip), `FirstUserMessage`, `FirstUserMessage_SkipsResume`, `ContentBlocks_StringContent`, `TextContent`, `ParseParentUUID_ContinuedSession`, `ParseParentUUID_NoContinuation` |
| `scaffold/scaffold_test.go` | 6 | `CreatesVault`, `AdoptsExistingVibeVault`, `RefusesExistingObsidian`, `RefusesExistingVibeVault`, `VaultNameReplacement`, `ExecutablePermissions` |
| `config/config_test.go` | 16 | `DefaultConfig`, `Load_NoConfig`, `Load_ValidConfig`, `Load_FrictionConfig`, `Load_FrictionConfigAbsent`, `Load_ExpandsHome`, `Load_XDGPriority`, `Load_InvalidTOML`, `Overlay_TagsOverride`, `Overlay_PartialOverride`, `Overlay_MissingFile`, `Overlay_FullyCommented`, `WithProjectOverlay`, `SessionTag`, `SessionTags`, `ProjectsDir_StateDir` |
| `config/write_test.go` | 6 | `WriteDefault_CreatesConfig`, `WriteDefault_UpdatesExistingVaultPath`, `WriteDefault_UnchangedExisting`, `WriteDefault_PreservesAllSections`, `WriteDefault_MissingVaultPathKey`, `CompressHome` |
| `enrichment/enrichment_test.go` | 11 | `Truncate`, `BuildMessages`, `ParseResponse`, `ParseResponse_EmptyChoices`, `ParseResponse_BadJSON`, `ValidateTag`, `Generate_Disabled`, `Generate_NoAPIKey`, `Generate_MockServer`, `Generate_Timeout`, `Generate_ServerError` |
| `help/help_test.go` | 62 | Terminal output regression (all 18 subcommands incl. zed), FormatUsage, registry completeness (18 subcommands), roff structure (subcommands + hook + context + zed + templates subcommands), escapeRoff, ManName (incl. spaces), hook + context subcommand terminal output |
| `context/context_test.go` | 27 | Init (CreatesVaultFiles, CreatesRepoFiles, ClaudeSubdirSymlinks, WorkflowMD, IdempotentSkip, ForceOverwrite, ProjectOverride, VaultNotFound, ClaudeMDContent, GitignoreUpdated, GitignoreIdempotent, ProjectDetection, AgentctxSymlink, ClaudeMDSymlink, ClaudeSubdirsRelativeSymlinks, GitignoreAgentctx, VersionFile), Migrate (CopiesResume, CopiesHistory, CopiesTasks, CopiesLocalCommands, SkipsAlreadySymlinkedCommands, SkipsMissing, SkipsExistingVaultFiles, ForceOverwrite, UpdatesRepoFiles, PreservesOriginals) |
| `context/schema_test.go` | 10 | ReadVersion (Missing, Roundtrip, Invalid), WriteVersion (Creates, Overwrites), MigrationsFrom (Zero, One, Two, Three, Latest) |
| `context/sync_test.go` | 14 | Sync (LegacyProject, AlreadyCurrent, PartialMigration, DryRun, AllMode, PropagatesSharedCommands, ExistingCommandNotOverwritten, Idempotent), Migrate0to1, Migrate1to2 (CreatesSymlink, RewritesCLAUDEMD, RelativeCommands, VaultOnlySkipsRepo), DiscoverProjects, PropagateSharedCommands |
| `context/template_test.go` | 9 | ResolveTemplate (Fallback, VaultOverride, VarSubstitution), ApplyVars, EnsureVaultTemplates (Creates, SkipsExisting), DefaultVars, BuiltinTemplates_ContainsCLAUDE, ReadEmbedded |
| `inject/inject_test.go` | 13 | `BuildEmpty`, `BuildSummary`, `BuildSessions`, `BuildSessionsFewEntries`, `BuildOpenThreads`, `BuildDecisions`, `BuildFriction`, `FormatMarkdown`, `FormatJSON`, `RenderTokenBudget`, `RenderSectionsFilter`, `EstimateTokens`, `OpenThreadsResolution`, `SignificantWords` |
| `hook/handler_test.go` | 13 | `HandleInput_SessionEnd`, `HandleInput_SessionEnd_MissingTranscript`, `HandleInput_EventOverride`, `HandleInput_ClearReason` (now processed), `HandleInput_StopCreatesCheckpoint`, `HandleInput_StopThenSessionEnd`, `HandleInput_StopNoTranscript`, `HandleInput_StopMissingFile`, `HandleInput_UnknownEvent`, `HandleInput_EmptyEvent`, `HandleInput_SessionEnd_RefreshesContext`, `HandleInput_SessionEnd_NoFrictionAlert`, `InputJSON` |
| `hook/setup_test.go` | 43 | Hook install/uninstall (12): `Install_NoFile`, `Install_EmptyFile`, `Install_ExistingSettingsNoHooks`, `Install_PreservesExistingHooks`, `Install_Idempotent`, `Install_PartialHooks`, `Install_CreatesBackup`, `Install_MalformedJSON`, `Uninstall_RemovesHooks`, `Uninstall_PreservesOtherHooks`, `Uninstall_NotInstalled`, `Uninstall_CleansEmptyHooksMap`. MCP Claude (10): `InstallMCP_NoFile`, `InstallMCP_ExistingSettings`, `InstallMCP_PreservesExistingServers`, `InstallMCP_Idempotent`, `InstallMCP_CreatesBackup`, `UninstallMCP_Removes`, `UninstallMCP_PreservesOtherServers`, `UninstallMCP_CleansEmptyMap`, `UninstallMCP_NotInstalled`, `InstallMCP_WithHooks`. MCP Zed (7): `InstallMCPZed_NoFile`, `InstallMCPZed_Existing`, `InstallMCPZed_Idempotent`, `UninstallMCPZed`, `UninstallMCPZed_PreservesOtherServers`, `UninstallMCPZed_NotInstalled`, `InstallMCPZed_WithJSONC`. Unified install (8): `InstallMCPAll_BothDetected`, `InstallMCPAll_OnlyClaude`, `InstallMCPAll_OnlyZed`, `InstallMCPAll_NeitherDetected`, `InstallMCPAll_ClaudeOnlyFlag`, `InstallMCPAll_ZedOnlyFlag`, `UninstallMCPAll_BothDetected`, `InstallMCPAll_Idempotent`. JSONC (6): `StripJSONC_LineComments`, `StripJSONC_BlockComments`, `StripJSONC_TrailingCommas`, `StripJSONC_CommentsInsideStrings`, `StripJSONC_ZedStyleSettings`, `StripJSONC_StrictJSON` |
| `index/index_source_test.go` | 3 | `SourceName` (empty defaults to claude-code, zed source, custom source) |
| `index/index_test.go` | 32 | `IndexBackwardsCompat`, `IndexEnrichedRoundTrip`, `IndexTranscriptPathRoundTrip`, `IndexCheckpointRoundTrip`, `IndexToolCountsRoundTrip`, `IndexTokensRoundTrip`, `IndexCommitsRoundTrip`, `IndexFrictionRoundTrip`, `Rebuild` (incl. token backfill assertions), `RebuildSkipsMalformed`, `RebuildSkipsNonSessionFiles`, `RelatedSharedFiles`, `RelatedThreadResolution`, `RelatedSameBranch`, `RelatedSameBranchMainExcluded`, `RelatedSameTag`, `RelatedMinScoreFilter`, `RelatedPreviousExclusion`, `RelatedMaxResults`, `SignificantWords`, `ProjectContextTimeline`, `ProjectContextDecisionDedup`, `ProjectContextThreadResolution`, `ProjectContextKeyFiles`, `Projects`, `IndexSaveLoad`, `ProjectContextFrictionAlert`, `ProjectContextFrictionAlertBelowThreshold`, `ProjectContextFrictionAlertDisabled`, `IndexParentUUIDRoundTrip`, `IndexParentUUIDOmitEmpty`, `ProjectContextContinuedSession` |
| `index/generate_test.go` | 4 | `GenerateContext_WritesHistoryMd`, `GenerateContext_WritesKnowledgeMd` (per-project knowledge.md seeding), `GenerateContext_NoSessions`, `GenerateContext_MultipleProjects` |
| `narrative/segment_test.go` | 5 | `NoCompaction`, `SingleBoundary`, `MultipleBoundaries`, `BoundaryExcluded`, `EmptyInput` |
| `narrative/extract_test.go` | 35 | `Extract_Nil`, `Extract_Empty`, `ClassifyToolUse` (Write, Edit, Read, PlanMode, AskUserQuestion, Task), `ClassifyBash` (Test, TestFailed, GitCommit, GitPush, Build, General), `IsTestCommand`, `AggregateExploration`, `DetectRecoveries` (found, too far), `ExtractCommitMessage`, `BuildToolResultMap`, `FirstUserRequest` (SkipsNoise, SkipsMeta), `ShortenPath`, `TruncateStr`, `FirstLine`, `ParseCommitResult` (6 subtests), `ExtractCommits` (single, failed, none, multiple), `Extract_PopulatesCommits`, `WriteToKnownFile_Modify`, `WriteToNewFile_Create`, `SecondWriteSamePath_Modify`, `ExtractKnownFiles` |
| `narrative/infer_test.go` | 36 | `InferTitle` (FromSegment, SkipsEmpty, FallsBackTranscript, FallsBackSession, PlanningFallback, FileCreateFallback, ExplorationFallback), `InferSummary` (CommitPrefix, TitleFallback, TestsFailed, MixedTests, CommitAndPush, CommitOnly, ErrorRecoveries, NoActivities, SessionTitle), `InferIntentPrefix` (FromLastCommit, FromActivities, PlanMode, Explore), `InferSubject` (FromLastCommit, FromTitle, EmptyTitle), `ExtractConventionalPrefix`, `StripConventionalPrefix`, `FormatOutcomes`, `InferTag` (6 variants), `InferOpenThreads` (3 variants), `ExtractDecisions`, `IsNoiseMessage`, `BacktickExtraction` |
| `narrative/render_test.go` | 5 | `Empty`, `SingleSegment`, `MultiSegment`, `SkipsEmptySegments`, `FilteredLongSession` |
| `prose/prose_test.go` | 19 | `Extract_Nil`, `Extract_Empty`, `Extract_SingleUserTurn`, `Extract_SingleAssistantTurn`, `Extract_FillerFiltered`, `Extract_LongTextKept`, `Extract_PureTextKept`, `Extract_UserNoiseFiltered`, `Extract_ToolResultSkipped`, `Extract_ThinkingSkipped`, `Extract_SystemSkipped`, `Extract_MetaSkipped`, `Extract_PlanContent`, `Extract_TurnOrder`, `Extract_TestMarker`, `Extract_CommitMarker`, `Extract_FileCreateMarker`, `Extract_SegmentBoundary`, `Extract_UserTruncation` |
| `prose/render_test.go` | 8 | `Render_Empty`, `Render_SingleUserTurn`, `Render_UserAndAssistant`, `Render_Markers`, `Render_ConsecutiveMarkers`, `Render_MultiSection`, `Render_SingleSection`, `Render_UserTruncation` |
| `stats/stats_test.go` | 19 | `Compute_Empty`, `Compute_SingleEntry`, `Compute_MultipleEntries`, `Compute_Averages`, `Compute_AveragesDivisionByZero`, `Compute_ProjectFilter`, `Compute_ProjectBreakdown`, `Compute_ModelBreakdown`, `Compute_TagBreakdown`, `Compute_TopFiles`, `Compute_TopFilesProjectThreshold`, `Compute_MonthlyTrend`, `Format_Overview`, `Format_Empty`, `Format_EmptyWithProject`, `Format_ProjectOmitsProjectsSection`, `FormatTokens`, `FormatDuration`, `FormatInt` |
| `stats/cost_test.go` | 7 | `EstimateCost_Disabled`, `EstimateCost_NoModels`, `EstimateCost_NoMatch`, `EstimateCost_BasicComputation`, `EstimateCost_WithCache`, `EstimateCost_FirstMatchWins`, `EstimateCost_ZeroTokens` |
| `stats/tools_test.go` | 8 | `AnalyzeTools_NoActivities`, `AnalyzeTools_AllSuccess`, `AnalyzeTools_WithErrors`, `AnalyzeTools_StruggleDetection`, `AnalyzeTools_NoStruggleBelowThreshold`, `RenderToolEffectiveness`, `RenderToolEffectiveness_Nil`, `ExtractFilePath`, `CountEditCycles` |
| `stats/export_test.go` | 8 | `ExportEntries_All`, `ExportEntries_Filtered`, `ExportEntries_Empty`, `ExportJSON_Data`, `ExportJSON_Empty`, `ExportCSV_Header`, `ExportCSV_Data`, `ExportCSV_Filtered` |
| `trends/trends_test.go` | 29 | `ComputeEmpty`, `ComputeEmptyMap`, `ComputeSingleSession`, `ComputeTwoWeeks`, `ComputeProjectFilter`, `ComputeFrictionAverage`, `ComputeTokensPerFile`, `ComputeSkipsZeroFriction`, `ComputeSkipsZeroDuration`, `ComputeRollingAverage`, `ComputeAnomalyDetection`, `ComputeDirection`, `ComputeDisplayWeeksLimit`, `ComputeSkipsBadDates`, `FormatEmpty`, `FormatEmptyProject`, `FormatSectionsPresent`, `FormatAnomalyMarker`, `FormatProjectInHeader`, `RollingAvg`, `RollingAvgShortWindow`, `MetricDirection`, `MetricDirectionStable`, `ISOWeekStart`, `WeekLabel`, `FormatFrictionThresholdMarker`, `FormatFrictionBelowThreshold`, `FormatFrictionThresholdDisabled`, `FormatFrictionOverviewThreshold` |
| `llm/retry_test.go` | 6 | `WithRetry_TransientRetry`, `WithRetry_PermanentNoRetry`, `WithRetry_ContextCancel`, `WithRetry_NamePassthrough`, `WithRetry_Success`, `WithRetry_BothFail` |
| `llm/openai_test.go` | 3 | `OpenAI_Success`, `OpenAI_TransientRetry`, `OpenAI_PermanentError` |
| `llm/anthropic_test.go` | 3 | `Anthropic_Success`, `Anthropic_Headers`, `Anthropic_SystemField` |
| `llm/google_test.go` | 3 | `Google_Success`, `Google_ResponseParsing`, `Google_Types` |
| `narrative/timeline_test.go` | 5 | `RenderTimeline_Empty`, `RenderTimeline_TrivialSession`, `RenderTimeline_Renders`, `RenderTimeline_SkipsZeroTimestamp`, `ActivityIcon` |
| `index/lock_test.go` | 3 | `Lock_Unlock`, `Lock_ConcurrentAccess`, `Lock_StaleLock` |
| `friction/detect_test.go` | 18 | `DetectCorrections_Nil`, `Negation`, `Redirect`, `Undo`, `Quality`, `Repetition`, `ShortNegation` (contextual), `NoFalsePositives`, `Multiple`, `StopRedirect`, `WaitRedirect`, `StillBroken`, `RollBack`, `IMeant`, `NegationExempt_Decision`, `NegationExempt_Preference`, `NegationExempt_ISaidWeShouldExempted`, `RealCorrectionStillCaught` |
| `friction/score_test.go` | 18 | `Score_ZeroSignals`, `Score_MaxSignals`, `CorrectionDensityOnly`, `TokenEfficiencyOnly`, `FileRetryOnly`, `ErrorCycleOnly`, `RecurringThreadsOnly`, `HalfThresholds`, `ClampAtMax`, `PartialCombination`, `Clamp`, `TopContributors_AllAtThreshold`, `TopContributors_SingleSignal`, `TopContributors_ZeroSignals`, `DynamicThreshold_ShortSession`, `DynamicThreshold_BaseSession`, `DynamicThreshold_LongSession`, `DynamicThreshold_ZeroDuration` |
| `friction/analyze_test.go` | 14 | `Analyze_NilInputs`, `CorrectionsOnly`, `NarrativeSignals`, `RecurringThreads_Jaccard`, `NoRecurringThreads_Jaccard`, `Combined`, `BuildSummary`, `HasRecurringThreads`, `HasRecurringThreads_NoMatch`, `SignificantWords`, `JaccardIdentical`, `JaccardNone`, `JaccardPartial`, `JaccardThreshold` |
| `friction/format_test.go` | 8 | `ComputeProjectFriction_Empty`, `SingleProject`, `ProjectFilter`, `SkipsZero`, `Format_Empty`, `Format_SingleProject`, `Format_ProjectFilter`, `Format_MultiProject` |
| `noteparse/noteparse_test.go` | 8 | `ParseFrontmatter`, `ParseBracketList`, `ExtractDecisions`, `ExtractOpenThreads`, `ExtractFilesChanged`, `ExtractCommits`, `MissingFrontmatter`, `EmptyFile` |
| `render/markdown_test.go` | 23 | `SessionNote_AllFields`, `SessionNote_MinimalFields`, `SessionNote_TagRendering`, `SessionNote_SessionTagsOverride`, `SessionNote_ToolUsage`, `SessionNote_CheckpointStatus`, `SessionNote_NoTools`, `SessionNote_PreviousNote`, `SessionNote_RelatedNotes`, `SessionNote_YAMLEscape`, `NoteDataFromTranscript`, `NoteDataFromTranscript_ZeroTime`, `NoteFilename`, `NoteRelPath`, `SessionNote_ProseDialogue`, `SessionNote_ProseDialogueFallback`, `SessionNote_Commits`, `SessionNote_NoCommits`, `SessionNote_FrictionSignals`, `SessionNote_NoFriction`, `SessionNote_LowFrictionNoSection`, `SessionNote_ParentSession`, `SessionNote_NoParentSession` |
| `sanitize/redact_test.go` | 5 | `StripTags_NoTags`, `StripTags_AllTagTypes` (18 subtests), `StripTags_NonMatchingTags`, `StripTags_NestedContent`, `StripTags_EmptyAndWhitespace` |
| `discover/discover_test.go` | 5 | `DiscoverFindsTranscripts`, `DiscoverSubagentDetection`, `DiscoverUUIDFiltering`, `FindBySessionID`, `FindBySessionIDSubagent` |
| `zed/parser_test.go` | 12 | `ParseThread_Valid`, `ParseThread_CorruptData`, `ParseThread_InvalidJSON`, `ParseDB_ValidThread`, `ParseDB_Empty`, `ParseDB_FilterSince`, `ParseDB_Limit`, `ParseDB_SkipsCorruptThread`, `ParseDB_NonexistentDB`, `ParseThread_UpdatedAtParsing`, `ParseThread_MessageEnumParsing`, `ParseThread_ToolUseAndResults`, `ParseThread_ResumeMarkerSkipped` |
| `zed/clock_test.go` | 8 | DESIGN #95 Clock-injection seam: `TestFakeClock_AdvanceFiresExpiredTimers`, `_AdvanceShortOfDeadlineDoesNotFire`, `_StopBeforeFireReturnsTrue`, `_StopAfterFireReturnsFalse`, `_RegisteredCountsAllAfterFunc`, `_PendingExcludesStoppedAndFired`, `_CallbackCanRegisterMoreTimers`, `_StopRaceWithAdvance` (state-consistency invariant: each timer ends `fired XOR cancelled`, never inconsistent — catches regressions in the back-pointer mutex even without `-race`). |
| `zed/convert_test.go` | 15 | `Convert_BasicThread`, `Convert_NilThread`, `Convert_ToolNormalization`, `Convert_TokenAggregation`, `Convert_NilTokenUsage`, `Convert_NilSnapshot`, `Convert_WithSnapshot`, `Convert_ThinkingBlocks`, `Convert_EmptyThread`, `Convert_MentionInUserMessage`, `Convert_ModelFormatting` (4 subtests), `Convert_BranchFallback`, `Convert_EndTime`, `Convert_ToolResultsOnAgentMessage` |
| `zed/detect_test.go` | 6 | `DetectProject_ValidSnapshot`, `DetectProject_NilSnapshot`, `DetectProject_BranchFallback`, `DetectProject_DomainDetection` (5 subtests), `DetectProject_EmptyWorktrees`, `DetectProject_SnapshotBranchPrecedence` |
| `zed/narrative_test.go` | 13 | `ExtractNarrative_NilThread`, `ExtractNarrative_EmptyMessages`, `ExtractNarrative_SummaryFromDBColumn`, `ExtractNarrative_DetailedSummaryPreferred`, `ExtractNarrative_SummaryCapped`, `ExtractNarrative_ToolActivities`, `ExtractNarrative_CommitExtraction`, `ExtractNarrative_ErrorDetection`, `ExtractNarrative_GitCommitActivity`, `ExtractNarrative_TagInference` (3 subtests), `ExtractNarrative_FirstUserRequest`, `ExtractNarrative_WorkPerformed`, `ParseCommitOutput` (5 subtests) |
| `zed/prose_test.go` | 12 | `ExtractDialogue_NilThread`, `ExtractDialogue_EmptyMessages`, `ExtractDialogue_BasicConversation`, `ExtractDialogue_ThinkingExcluded`, `ExtractDialogue_ToolMarkers`, `ExtractDialogue_MentionsAsAtPath`, `ExtractDialogue_FillerFilter`, `ExtractDialogue_LongUserTextCapped`, `ExtractDialogue_BashMarkers` (4 subtests), `ExtractDialogue_ErrorMarker` |
| `effectiveness/effectiveness_test.go` | 14 | `Analyze_EmptyIndex`, `Analyze_NoContextData`, `Analyze_CohortAssignment`, `Analyze_NegativeCorrelation`, `Analyze_ProjectFilter`, `PearsonR` (4 subtests), `BackfillContext_PopulatesEmpty`, `BackfillContext_SkipsExisting`, `BackfillContext_MultiProject`, `BackfillContext_SortOrder`, `BackfillContext_HasHistoryFalse`, `Format` |
| `archive/archive_test.go` | 3 | `ArchiveRoundTrip`, `IsArchived`, `ArchivePath` |
| `mcp/tools_test.go` | 54 | `GetProjectContextBasic`, `GetProjectContextWithSections`, `GetProjectContextEmptyIndex`, `GetProjectContextDefaultMaxTokens`, `ListProjectsBasic`, `ListProjectsEmptyIndex`, `SearchSessionsQueryFilter`, `SearchSessionsProjectFilter`, `SearchSessionsDateFilter`, `SearchSessionsFrictionFilter`, `SearchSessionsFileFilter`, `SearchSessionsMaxResults`, `SearchSessionsEmpty`, `SearchSessionsCombinedFilters`, `GetKnowledgeBasic`, `GetKnowledgeMissing`, `GetKnowledgePathTraversal`, `GetKnowledgeEmptyProject`, `GetSessionDetailBasic`, `GetSessionDetailIteration`, `GetSessionDetailMissing`, `GetSessionDetailPathTraversal`, `GetSessionDetailBadDate`, `GetFrictionTrendsBasic`, `GetFrictionTrendsEmpty`, `GetFrictionTrendsCustomWeeks`, `GetEffectivenessBasic`, `GetEffectivenessEmpty`, `CaptureSessionTool_Success`, `CaptureSessionTool_MissingSummary`, `CaptureSessionTool_MinimalInput`, `FirstSentence`, `ValidateProjectName` |
| `mcp/tools_context_test.go` | 26 | `GetWorkflowBasic`, `GetWorkflowFallbackToTemplate`, `GetWorkflowPathTraversal`, `GetResumeBasic`, `GetResumeMissing`, `GetResumePathTraversal`, `ListTasksBasic`, `ListTasksIncludeDone`, `ListTasksEmpty`, `ListTasksPathTraversal`, `ListTasksStatusFormats`, `GetTaskBasic`, `GetTaskFallbackToDone`, `GetTaskFallbackToCancelled`, `GetTaskNotFound`, `GetTaskPathTraversal`, `GetTaskMissingName`, `BootstrapContextBasic`, `BootstrapContextMissingResume`, `BootstrapContextWorkflowFallback`, `BootstrapContextNoTasks`, `BootstrapContextTokenBudget`, `BootstrapContextPathTraversal`, `ResolveProjectExplicit`, `ResolveProjectInvalidExplicit`, `ValidateTaskName` |
| `mcp/tools_context_write_test.go` | 28 | `UpdateResumeBasic`, `UpdateResumeSectionNotFound`, `UpdateResumeFileNotFound`, `UpdateResumePreservesOtherSections`, `UpdateResumeLastSection`, `UpdateResumePathTraversal`, `AppendIterationAutoIncrement`, `AppendIterationExplicitNumber`, `AppendIterationDuplicateNumber`, `AppendIterationCreatesFile`, `AppendIterationInvalidDate`, `AppendIterationDefaultDate`, `ManageTaskCreate`, `ManageTaskCreateAlreadyExists`, `ManageTaskCreateNoContent`, `ManageTaskUpdateStatus`, `ManageTaskUpdateStatusNoStatus`, `ManageTaskUpdateStatusNotFound`, `ManageTaskRetire`, `ManageTaskRetireNotFound`, `ManageTaskUnknownAction`, `ManageTaskPathTraversal`, `RefreshIndexBasic`, `RefreshIndexEmptyVault`, `ReplaceStatusPlainFormat`, `ReplaceStatusHeadingFormat`, `IterationHeadingRoundTrip`, `ScanIterationNumbers` |
| `check/check_test.go` | 27 | `CheckVaultPath` (pass/fail), `CheckObsidian` (pass/warn), `CheckProjects` (pass/warn), `CheckStateDir` (pass/warn), `CheckIndex` (pass/warn/fail), `CheckDomains` (all exist/some missing/empty skipped), `CheckEnrichment` (disabled/enabled+key/enabled+no key), `checkHookFile` (pass/warn/fail), `Report.HasFailures` (true/false), `Run` integration, `Status.String`, `CheckAgentctxSchema` (current/outdated/no-agentctx) |
| `templates/templates_test.go` | 8 | `New` (entry count=14), `DefaultContent`, `DefaultContentReturnsCopy`, `Has`, `Compare` (default/customized/missing), `Reset` (create/reset), `ResetAll` (14 actions), `ResetUnknown` |
| `vaultsync/vaultsync_test.go` | 22 | `Classify` (15 subtests: history.md, session-index.json, session notes, templates, config, knowledge.md, resume.md, iterations.md, tasks, dashboards, README), `GetStatus_CleanRepo`, `GetStatus_DirtyRepo`, `CommitAndPush_NoRemote`, `CommitAndPush_NothingToCommit`, `Pull_NoRemote`, `EnsureRemote` |
| `vaultsync/vaultsync_test.go` (convergence) | `CommitAndPush_SHADivergenceConvergence_GithubFirst` | second-iterated remote rejects; assert both bare refs converge to the same SHA after rebase + force-with-lease. |
| `vaultsync/vaultsync_test.go` (convergence) | `CommitAndPush_SHADivergenceConvergence_RejecterFirst` | first-iterated remote rejects (alphabetical-ordering coverage with `aaa-rejecter` / `zzz-acceptor`); assert both bare refs converge. |
| `vaultsync/vaultsync_test.go` (convergence) | `CommitAndPush_LeaseRejectsConcurrentWriter` | `afterPushHook` plants a third-party commit on github's bare mid-flight; assert the convergence lease rejects with `"convergence rejected"`, `AllPushed()` is false, and the bare is left at the third-party state (no overwrite). |
| `vaultsync/vaultsync_test.go` (convergence) | `CommitAndPush_BothRemotesRebase` | both remotes carry distinct unrelated commits; sequential rebase chain; assert both bare refs match AND `PushResult.CommitSHA` reflects the post-loop HEAD (covers the v1-H2 refresh). |
| `vaultsync/vaultsync_test.go` (convergence) | `CommitAndPush_ThreeRemotesSecondCascade` | three bare remotes, two with unrelated commits; asserts second-cascade re-convergence (the third remote's rebase re-converges both prior remotes under fresh leases) even though the live vault is N=2. |
| `vaultsync/vaultsync_test.go` (convergence) | `AfterPushHook_DefaultIsNoOp` | sanity: the default `afterPushHook` is non-nil and a no-op (folds in v1-M2). |
| `synthesis/actions_test.go` | 16 | `AppendLearnings_NewEntry`, `AppendLearnings_DuplicateSkipped`, `AppendLearnings_MissingSection`, `AppendLearnings_MissingFile_SeedsTemplate`, `AppendLearnings_EmptySection`, `FlagStaleEntries_IndexMatch`, `FlagStaleEntries_FuzzyFallback`, `FlagStaleEntries_NoMatch`, `FlagStaleEntries_AlreadyFlagged`, `UpdateResume_BothSections`, `UpdateResume_OneSection`, `UpdateResume_MissingFile`, `ApplyTaskUpdates_Complete`, `ApplyTaskUpdates_UpdateStatus`, `ApplyTaskUpdates_MissingTask`, `Apply_FullWorkflow` |
| `synthesis/prompt_test.go` | 9 | `BuildPrompt_AllSections`, `BuildPrompt_EmptyKnowledge`, `BuildPrompt_EmptyResume`, `BuildPrompt_GitDiffTruncation`, `BuildPrompt_NoTasks`, `BuildPrompt_NoHistory`, `BuildPrompt_NumberedBullets`, `BuildPrompt_NoCommitsNoDiff`, `NumberBullets_ResetsBetweenSections` |
| `synthesis/synthesize_test.go` | 9 | `Synthesize_FullResult`, `Synthesize_EmptyResult`, `Synthesize_NilProvider`, `Synthesize_LLMError`, `Synthesize_InvalidJSON`, `Synthesize_InvalidSection`, `Synthesize_InvalidTaskAction`, `Synthesize_InvalidStaleFile`, `Synthesize_NegativeIndex` |
| `synthesis/run_test.go` | 5 | `Run_NilProvider`, `Run_Disabled`, `Run_EndToEnd`, `Run_LLMError`, `Run_EmptyResult` |
| `mdutil/mdutil_test.go` | 56 | `SignificantWords_Basic`, `SignificantWords_StopWords`, `SignificantWords_PunctuationTrimming`, `SignificantWords_ShortWordsFiltered`, `IsStopWord`, `Overlap_Matching`, `Overlap_DuplicatesInB`, `Overlap_NoMatch`, `Overlap_Empty`, `SetIntersection_Basic`, `SetIntersection_NoDuplicates`, `SetIntersection_Empty`, `ReplaceSectionBody_Basic`, `ReplaceSectionBody_NotFound`, `ReplaceSectionBody_LastSection`, `ReplaceSectionBody_PreservesOtherSections`, `AtomicWriteFile_CreatesDir`, `AtomicWriteFile_OverwritesExisting`; subsection family: `NormalizeSubheadingSlug_*` (8 variants), `ReplaceSubsectionBody_*` (10 variants incl. HTML comment, code fence), `InsertSubsection_*` (ModeTop, ModeBottom, ModeAfter, ModeBefore, and edge cases), `RemoveSubsection_*`. **DESIGN #92 hard-error multi-match coverage:** `TestReplaceSubsectionBody_AmbiguousMultiMatch` and `TestRemoveSubsection_MultiMatch` rewritten to assert `err != nil` (slug-ambiguity error) and that the document is byte-unchanged on rejection (formerly returned a `candidates_warning:` prefix, now hard error). |
| `mdutil/carried_test.go` | 35 | Liberal-on-read corpus: `ParseCarriedForward_Empty`, `_SingleCanonical`, `_VariantCanonical`, `_VariantBoldColon`, `_VariantBoldParen`, `_VariantEmDash`, `_VariantPlain`, `_VariantPlainNoSentenceMark`, `_MultilineBoldSlug`, `_MultiBullet`, `_ContinuationLines`; round-trip: `_RoundTrip_*` (5 variants), `_TwelveBullets`; mutation: `AddCarriedBullet_*` (6), `RemoveCarriedBullet_*` (6), `GetCarriedBullet_*` (2), `BuildCarriedBullet_*` (2), `ParseLiberalVariants_AllFive` |
| `meta/project_root_test.go` | 8 | `TestProjectRoot_GitDir`, `_AgentctxDir`, `_VaultOnly`, `_VaultRootRefused`, `_NotFound`, `_Override`, `_WorktreeGitFile`, `_AgentctxBeforeGit` |
| `mcp/tools_project_test.go` | 3 | `TestGetProjectRoot_GitDir`, `_AgentctxDir`, `_VaultRootRefused` |
| `mcp/tools_commit_msg_test.go` | 5 | `TestSetCommitMsg_CreatesMissing`, `_OverwritesExisting`, `_ProjectPathRequired`, `_ContentRequired`, `_PartialSuccessDiagnostic` |
| `mcp/tools_thread_test.go` | 19 | `TestThreadInsert_*` (ModeTop, ModeBottom, ModeAfter, SlugAlreadyExists, MissingSlug, ProjectNotFound), `TestThreadReplace_*` (Basic, SlugWithEmDash, SlugNotFound, CarriedForwardRejected, AmbiguousMultiMatch, HTMLCommentInBodyRejected, MissingSlug), `TestThreadRemove_*` (Basic, CarriedForwardRejected, SlugNotFound, AmbiguousMultiMatch, MissingSlug, EmptyOpenThreads). `_AmbiguousMultiMatch` rewrites under DESIGN #92 — the handler now returns an error mentioning slug ambiguity (rather than surfacing a `candidates_warning:` prefix); resume.md remains byte-unchanged on rejection. The `extractCandidatesWarning` helper and the `CandidatesWarning` JSON field on the thread result type retired with the mdutil hard-error change. |
| `mcp/tools_carried_test.go` | 22 | `TestCarriedAdd_*` (ToEmpty, ToSingle, ToMulti, SlugAlreadyExists, SlugAlreadyExists_CaseInsensitive, MissingSlug, MissingTitle, ProjectNotFound, CanonicalBulletForm), `TestCarriedRemove_*` (Single, Multi_First, Multi_Last, CaseInsensitive, SlugNotFound, MissingSlug), `TestCarriedPromote_*` (Basic, SingleBullet, SlugNotFound, TaskAlreadyExists, MissingSlug, MissingNewTaskSlug, TaskFrontmatterShape) |
| `mcp/tools_agents_test.go` | 4 | Phase 2 MCP read path for the embedded agent registry: `TestVVGetAgentDefinition_HappyPath`, `_NotFound`, `_RoundTrip`, `_MissingName` |
| `agentregistry/registry_test.go` | 7 | In-binary registry: `TestLookup_HappyPath`, `TestLookup_MissingName`, `TestList_ReturnsSorted`, `TestParse_FrontmatterEdgeCases`, `TestSha256_Stable`, `TestSha256_FieldOrderIndependent`, `TestParseAgent_RoundTrip`. The `wrap-executor` agent definition retired with the dispatch ladder in DESIGN #92; the registry's `agents/` directory is currently empty. Tests cover the registry / parser / sha256 surfaces against synthesized in-test agent fixtures. |
| `llm/anthropichttp_test.go` | 4 | Phase 1 shared HTTP plumbing core (C1-v2 fix): `TestAnthropicHTTPCore_DoSendsExpectedHeaders`, `_DoMergesExtraHeaders`, `_DoPostsToCorrectPath`, `_DefaultBaseURL` |
| `cmd/vv/internal_test.go` | 2 | `vv internal generate-agents` generator: `TestGenerator_Idempotent` (regenerate same input → byte-identical output), `TestGenerator_ContainsBanner` (operator-facing comment present). The agent registry is empty under DESIGN #92; the generator surface remains as scaffolding for any future agent-flow feature. |
| `config/config_test.go` (`[wrap]` schema, post-Direction-C) | 6 | `[wrap].tiers` map + `default_model` survive (used by `vv_render_wrap_text` for tier-string lookup): `TestLoad_PartialMapOverride` (Load path via toml lib), `TestOverlay_PartialMapMerge` (in-memory Overlay path — H3-v3 map-merge fix), `TestOverlay_WrapDefaultModelAndLadder`, `TestValidate_HappyPath`, `_BadTierFormat`, `_EmptyWrapSection`, `TestDefaultConfig_WrapSection`. The `EscalationLadder` field retired in DESIGN #92; legacy keys silently no-op via the BurntSushi/toml non-strict default with a one-line stderr deprecation log. The `_NonAnthropicProviderRejected` v1 constraint also retired (Direction-C accepts any tier-string prefix that resolves through `llm.NewProvider`). |
| `cmd/wrap-trace/main_test.go` | 3 | `TestAnalyse_GoldenWrap` (golden-file output comparison), `TestMeasure_GoldenFile` (per-step latency JSON structure), `TestCanonicalTool` (tool normalization) |
| `llm/keyresolver_test.go` | 4 | DESIGN #89 layered API key resolution: `TestResolveAPIKey_ConfigWins` (config beats env for all three providers), `_EnvFallback` (config empty → env returned), `_BothEmpty_ActionableError` (error names both `vv config set-key <provider>` and the env var, all three providers), `_UnknownProvider` (rejects with supported-set message) |
| `config/config_test.go` (Phase 1 dispatch-api-key additions) | 5 | `[providers]` schema + overlay merge: `TestLoad_ProvidersSection`, `_NoProvidersSection`, `_EmptyProvidersSection`, `TestOverlay_ProvidersAllThreeMerge`, `_ProvidersFieldByField` (DESIGN #89) |
| `config/write_test.go` (Phase 5 addition) | 1 | `TestWriteDefault_PreservesProviderKeys` — re-running `vv init` against a config carrying `[providers.<P>].api_key` for all three providers leaves every key unchanged. Locks H3 from the v2 plan review (DESIGN #89). |
| `llm/provider_test.go` (Phase 2 addition) | 1 | `TestNewProvider_ConfigKeyForwarded` — `NewProvider(enrich, providers)` wires the config-stored key into the provider constructor, with env-fallback preserved when config is empty (DESIGN #89). |
| `wraprender/markers_test.go` | 18 | DESIGN #90 marker-block renderer for resume.md state-derived sub-regions (now driven by DESIGN #92 D4b auto-heal hooks instead of the retired `ApplyBundle` Step 9): `TestRenderActiveTasks_*` (Empty zero-task fallback, OneTask, MultipleTasks, StableOrdering priority-desc-then-slug-asc, TitleEscaping for markdown specials), `TestRenderCurrentState_AllFields`, `_OutputPassesV10Validator` (calls `context.ValidateCurrentStateBody` on rendered Iterations/MCP/Embedded bullets and asserts ok=true; locks Option-C scope reduction — the Tests bullet is deliberately NOT emitted), `TestRenderProjectHistoryTail_TruncatesToN` / `_HandlesEmptyIterations`, `TestApplyMarkerBlocks_ReplacesExisting`, `_InsertsWhenAbsent` (4 sub-cases: active-tasks under Open Threads, current-state under Current State, project-history-tail under Project History, active-tasks replaces existing H3), `_PreservesOutsideRegions` (byte-equality outside marker spans), `_Idempotent` (run twice → identical), `_HandlesMissingEndMarker` (parse error). |
| `mcp/tools_describe_iter_state_test.go` | 17 | DESIGN #92 + DESIGN #93 minimal iter-state record (anchor now stamp-file sourced): `TestDescribeIterState_Basic`, `_DirtyVault` (`vault_has_uncommitted_writes` true when porcelain non-empty), `_PriorIterAnchorFound` (reseeded — writes `.vibe-vault/last-iter` instead of an `## Iteration N` H2 footer), `_BranchDetection` (`git rev-parse --abbrev-ref HEAD`), `_InvalidProjectName` (validateProjectName rejection), `_NoVaultGit` (graceful empty `last_iter_anchor_sha`), `TestLastIterAnchorSha_StampFileFound` (returns the stamp commit's SHA, not a later unrelated commit), `_StampFileMissing_ReturnsEmpty` (no stamp ever written → empty), `_StampFileUntracked_ReturnsEmpty` (file on disk but not `git add`-ed → empty), `_StampFileMultipleVersions_ReturnsLatest` (newest commit touching the stamp wins), `_StampPreservedAcrossRebase` (regression-lock for the retired `wrap-shape-rebase-merge-not-recognized` thread), `_NoGit_ReturnsEmpty` (non-git cwd tolerated), `_EmptyCwd_ReturnsEmpty`, `TestGitCmdRunner_Defaulted` (default runner uses real git), `TestDescribeIterStateTool_OutputJSONShape` (4-field shape lock), `TestNextIterFromIterationsMD` (6 subtests covering missing file, no headers, single header, many headers, out-of-order, h2 ignored), `_EmptyArgs`. The deleted `TestLastIterAnchorSha_NoMatch` and `_TargetIterZero` (which exercised the retired body-grep loop and the now-removed `targetIter` argument) retire with DESIGN #93. |
| `mcp/tools_stamp_iter_test.go` | 9 | DESIGN #93 `vv_stamp_iter` MCP tool: `TestStampIter_Basic` (writes `170\n` to `.vibe-vault/last-iter` at the project root), `_Idempotent` (second write succeeds, content unchanged), `_RejectsZero`, `_RejectsNegative` (iter must be ≥ 1), `_RejectsMissingProjectPath`, `_RejectsRelativeProjectPath` (absolute-path validation mirrors `vv_set_commit_msg`), `_CreatesParentDir` (`os.MkdirAll(.vibe-vault/, 0o755)`), `_AtomicReplace` (overwrite `169` with `170` via `atomicWriteFile`'s temp-file + rename — no torn intermediate state), `_ContentExactlyIntegerNewline` (file ends with exactly one trailing `\n`). The shared `atomicWriteFile` helper at `internal/mcp/tools_commit_msg.go` is exercised by both this tool and `vv_set_commit_msg`. |
| `mcp/tools_render_wrap_text_test.go` | 16 | DESIGN #92 unified context-aware renderer: `TestRenderWrapText_IterNarrativeKind` / `_CommitMsgKind` / `_BothKind` (3 kind variants with mocked Provider), `_BothKindRejectsIncomplete` (output missing required fields), `_InvalidKind` / `_MissingTier` / `_BadIterN`, `_CodeFenceTolerated` (LLM-emitted markdown fence stripped), `_CommitSubjectMustBeSingleLine` (newline rejection), `_ProviderError` (LLM HTTP error surfaces), `_MalformedJSON`, `_PromptIncludesIterState` (prompt-string assertion), `_XMLSpecialCharsRoundTrip` (D2 — `<sha>..<sha>`-shaped prose round-trips verbatim through `json.Unmarshal`), `TestRenderUserPrompt_GoldenSnapshot` (byte-stable prompt diffs), `_DefaultProviderFactoryRejectsEmpty` / `_DefaultProviderFactoryUnknownTier` (factory boundary). |
| `mcp/resume_state_blocks_test.go` | 9 | DESIGN #92 D4b auto-heal byte-identity coverage: `TestRenderResumeStateBlocks_OnDiskMatchesReturnValue` (helper output equals on-disk after write), `TestAutoHealAppendIteration_RegeneratesStateBlocks` (post-write hook fires after `vv_append_iteration`), `TestAutoHealUpdateResume_RegeneratesStateBlocks` (post-write hook fires after `vv_update_resume`), `TestAutoHealResumeStateBlocks_NoResumeMd` (graceful when resume.md missing), `TestRenderResumeStateBlocks_DeterministicAcrossRuns` (idempotence), `TestCollectActiveTasksReturnsAll` (helper coverage), `TestComputeCurrentState_HitsAllSources` (Iterations/MCP/Embedded), `TestCollectHistoryRowsRespectsLimit` (N=10), `TestSummarizeIterationNarrative` (history-row prose extractor). |
| `cmd/vv/config_setkey_test.go` | 12 | DESIGN #89 `vv config set-key` command: `TestConfigSetKey_FreshConfig` (creates with mode 0600 + parent dir 0700), `_AddProvider`, `_OverwriteRequiresForce`, `_OverwriteWithForce`, `_StdinDash`, `_StdinDashTrimsTrailingNewline`, `_RejectsKeyWithEmbeddedNewline`, `_RejectsEmptyKey`, `_UnknownProvider`, `_FileMode`, `_PreservesOtherLines`, `_TempFileSameDirectory` (atomic rename invariant — temp file in `filepath.Dir(configPath)`, not `/tmp/`) |
| `vaultfs/safety_test.go` | 18 | `TestValidateRelPath_*` (8 variants: RejectsAbsolute, RejectsDotDotSegment, RejectsNullBytes, RejectsControlChars, RejectsEmpty, RejectsDot, AcceptsTypicalRelative, AcceptsHiddenFile), `TestResolveSafePath_*` (4 variants: HappyPath, RealpathStaysUnderVault, RejectsSymlinkEscape_RealpathBased, RejectsAfterClean), `TestIsRefusedWritePath_*` (6 variants: RejectsGitSegmentTopLevel, RejectsGitSegmentNested, RejectsGitSegmentCaseInsensitive, AllowsSubstringNotSegment, AllowsGitignore, AllowsHiddenNonGit) |
| `vaultfs/read_test.go` | 21 | `TestRead_*` (8 variants: HappyPath, FileNotFound, SizeCapDefault, SizeCapCustom, SizeCapExceedsMax, PathTraversalRejected, FollowsSymlinkUnderVault, RejectsSymlinkEscape_Realpath), `TestList_*` (6 variants: HappyPath, NotADir, NotFound, HidesDotGit, HidesDotGitCaseInsensitive, IncludeSha256_OptIn), `TestExists_*` (5 variants: File, Dir, Missing, DanglingSymlink, Symlink_ResolvesUnderVault), `TestSha256_*` (2 variants: HappyPath, FileNotFound) |
| `vaultfs/write_test.go` | 28 | `TestWrite_*` (12 variants incl. HappyPath, FilePermissions_0o644, NoTempFileDebrisOnSuccess, NoTempFileDebrisOnRenameError, RefusesGitDir_TopLevel/Nested/CaseInsensitive, AllowsGitSubstring, CompareAndSet_Match/Mismatch/FileMissing, NoCompareAndSet_Overwrites, CreatesParentDirs), `TestEdit_*` (6: HappyPath, NotFoundString, AmbiguousMatch, ReplaceAll, RefusesGitDir, CompareAndSet_Mismatch), `TestDelete_*` (4: HappyPath, RefusesGitDir, OnDirectory, CompareAndSet_Mismatch), `TestMove_*` (5: HappyPath, DestinationExists, RefusesGitDir_Source, RefusesGitDir_Destination, SamePath) |
| `vaultfs/automemory_acceptance_test.go` | 4 | `TestVaultfs_AutoMemoryWrite_VisibleViaHostSymlink`, `_AutoMemoryRead_ViaHostSymlink_ReturnsVaultContent`, `_AutoMemoryEdit_PropagatesViaHostSymlink`, `_AutoMemoryDelete_VisibleViaHostSymlink` — production-mirror tests: tempdir vault `V` with `Projects/foo/agentctx/memory/` as a regular dir, separate tempdir `H` with `os.Symlink(V/.../memory, H/memory)`, verifies write/read/edit/delete via vault paths converge through the host-side symlink view |

## Integration Test

| File | Subtests | Coverage |
|------|----------|----------|
| `test/integration_test.go` (TestIntegration) | 32 | `init` (with nested `reinit_updates_vault_path`), `process_session_a1`, `process_session_a2_iteration`, `process_trivial_skipped`, `process_session_b_different_project`, `process_narrative_session`, `index_rebuild`, `index_knowledge_seeding`, `stats`, `backfill`, `archive`, `stop_checkpoint_then_session_end`, `process_friction_session`, `friction`, `trends`, `inject`, `context_init_and_migrate`, `context_sync`, `context_marker_guards`, `check_resume_invariants`, `export`, `reprocess`, `mcp`, `mcp_learnings`, `mcp_update_resume_guard`, `provenance_in_vault_writes`, `context_sync_t1_t2_cascade`, `memory_link_cli`, `vault_push_multi_remote`, `no_real_vault_mutation` |
| `test/integration_test.go` (TestIntegration_Vault\*) | 21 | Top-level tests for `vv_vault_*` MCP surface: `TestIntegration_VaultRead_HappyPath`, `_VaultRead_PathTraversal`, `_VaultRead_SymlinkEscape`, `_VaultList_HappyPath`, `_VaultList_HidesDotGit`, `_VaultList_IncludeSha256`, `_VaultExists_File`, `_VaultSha256_HappyPath`, `_VaultRead_TempVaultPath_ViaConfig`, `_VaultWrite_HappyPath`, `_VaultWrite_PathTraversal`, `_VaultWrite_RefusesGitDir`, `_VaultWrite_RefusesGitDir_CaseInsensitive`, `_VaultWrite_AllowsGitSubstring`, `_VaultWrite_CompareAndSet`, `_VaultEdit_HappyPath`, `_VaultEdit_AmbiguousMatch`, `_VaultDelete_HappyPath`, `_VaultDelete_RefusesDirectory`, `_VaultMove_HappyPath`, `_AutoMemoryWrite_VisibleViaHostSymlink` (full end-to-end through MCP) |

The integration test builds `vv` once via `TestMain`, then runs 32 sequential subtests under `TestIntegration`
exercising the full pipeline as subprocess calls. Uses XDG_CONFIG_HOME isolation and
temp directories. 8 JSONL fixtures loaded from `test/testdata/*.jsonl` via `readTestdata()`: normal session, same-day iteration,
trivial (skipped), different project, UUID-named backfill, checkpoint session, narrative session (with tool results), friction session (with correction patterns). The
`index_knowledge_seeding` subtest verifies per-project `knowledge.md` files are seeded during index generation. The `inject` subtest tests markdown/JSON output, sections filter, max-tokens truncation, help flag, and unknown project warning. The `export` subtest tests JSON output, project filter, CSV format, and help flag. The `context_sync` subtest verifies schema migration (0→2), shared command propagation, dry-run mode, and idempotent re-sync. Skipped in `-short` mode.

In addition to `TestIntegration`, the file holds 21 top-level `TestIntegration_Vault*` tests covering the eight `vv_vault_*` MCP tools through the same subprocess-driven harness: read/list/exists/sha256/write/edit/delete/move plus path-traversal, symlink-escape, `.git`-segment refusal (case-insensitive and substring-allowed), compare-and-set semantics, ambiguous-edit detection, and an end-to-end auto-memory shared-storage acceptance test that creates the host-side symlink and writes through the MCP surface.

The `mcp` subtest's `tools/list` check uses an **exact-set assertion**: the test enumerates all 39
expected tool names by name and compares against the server's reported list bidirectionally. A
tool missing from the expected list fails with `"unexpected tool %q"`, and a tool missing from the
server's list fails with `"missing expected tool %q"`. The old numeric `len(tools) != 20` check has
been replaced; add new tools explicitly to `expectedTools` in `test/integration_test.go` to prevent
silent count drift. The eight `vv_vault_{read,list,exists,sha256,write,edit,delete,move}` entries
land on the slice in the vault-accessor epic (count 31 → 39); the `wrap-model-tiering` epic
(DESIGN #83–#88) added four more wrap-bundle/dispatch tools to bring the count to 43, all of
which (plus `vv_render_commit_msg`, plus the two `vv_synthesize_wrap_bundle` /
`vv_apply_wrap_bundle_by_handle` handle-based replacements) retired in DESIGN #92's
Direction-C collapse. Direction-C added two new tools (`vv_describe_iter_state`,
`vv_render_wrap_text`), bringing the current count to 39.

## HOME-Sandbox Classification

Every first-party caller of `os.UserHomeDir()` / `os.Getenv("HOME")` /
`os.Getenv("USER")` outside `internal/config/` is classified into one of
three categories at the **call-site** level (a helper like
`plugin.ClaudePluginsDir` feeds both read-only and write-path callers,
which classify independently). Production-code authors adding a new such
call site must place it into this table and confirm it is covered by
either a HOME-sandboxed integration subtest or the canary below.

### Category A — Safe, no I/O on `$HOME`

Pure string/path computation. No sandboxing needed for correctness or
determinism.

| Site | Function | What it does |
|------|----------|--------------|
| `internal/sanitize/path.go:13` | `CompressHome` | String prefix swap (`$HOME/x` → `~/x`) |
| `internal/zed/detect.go:187` | `commonProjectRoot` | Depth-gate arithmetic on `os.UserHomeDir()` output |
| `internal/zed/detect.go:189` | `commonProjectRoot` fallback | `os.Getenv("USER")` fallback inside `UserHomeDir` error branch |
| `internal/meta/provenance.go:64` | `user()` | Env metadata fallback, no I/O |
| `cmd/vv/main.go:1367` (pure-compute callers) | `defaultTranscriptDir` | Pure string path; callers gate on non-empty |

### Category B — Read-only operator-private access

Reads files or lstats under `$HOME/.claude/`, `$HOME/.config/`,
`$HOME/.local/share/`, or `$HOME/.cache/` but never writes. Sandboxing
required for **test determinism** (output depends on operator state),
but no data-loss risk if unsandboxed.

| Site | Function | Path read |
|------|----------|-----------|
| `internal/check/check.go:278` | `CheckHook` | `~/.claude/settings.json` |
| `internal/check/check.go:299` | `CheckMCP` | `~/.claude/settings.json` |
| `internal/check/check.go:411` | `CheckMemoryLink` | `~/.claude/projects/<slug>/memory` (lstat) |
| `internal/hook/setup.go:27` via `claudeDetected` (L552) | `claudeDetected` | stat `~/.claude/` |
| `internal/hook/setup.go:168` via `zedDetected` (L562) | `zedDetected` | stat `~/.config/zed/` |
| `internal/plugin/plugin.go:51` via `AnyCacheInstalled` / `IsInstalled` | (plugin readers) | `~/.claude/plugins/cache/...` |
| `internal/zed/parser.go:21` | `DefaultDBPath` | `~/.local/share/zed/threads/threads.db` (opened `?mode=ro`) |
| `cmd/vv/main.go:1367` via `runBackfill` / `runReprocess` / `runZed` | `defaultTranscriptDir` | `~/.claude/projects/` (read-only scan) |

### Category C — Sandbox-needed (writes to operator-private paths)

**HIGHEST blast radius.** Any test regression that reaches one of these
sites without HOME-sandboxing mutates the operator's real config. Every
entry MUST have either a HOME-sandboxed integration subtest OR canary
snapshot coverage — never both zero.

| Caller | CLI entrypoint | Write target | Coverage |
|--------|----------------|--------------|----------|
| `hook.Install` (L37) | `vv hook install` | `~/.claude/settings.json` | canary |
| `hook.Uninstall` (L69) | `vv hook uninstall` | `~/.claude/settings.json` | canary |
| `hook.InstallMCP` (L104) | `vv mcp install` / `--claude-only` | `~/.claude/settings.json` | canary |
| `hook.UninstallMCP` (L137) | `vv mcp uninstall` / `--claude-only` | `~/.claude/settings.json` | canary |
| `hook.InstallClaudePlugin` (L637) | `vv mcp install --claude-plugin` | `~/.claude/settings.json` | canary |
| `hook.UninstallClaudePlugin` (L690) | `vv mcp uninstall --claude-plugin` | `~/.claude/settings.json` | canary |
| `hook.InstallMCPZed` (L178) | `vv mcp install --zed-only` | `~/.config/zed/settings.json` | canary |
| `hook.UninstallMCPZed` (L211) | `vv mcp uninstall --zed-only` | `~/.config/zed/settings.json` | canary |
| `plugin.InstallToCache` / `plugin.RegisterKnownMarketplace` / `plugin.RegisterInstalledPlugin` / `plugin.Remove` | `vv mcp install`/`uninstall` `--claude-plugin` | `~/.claude/plugins/cache/vibe-vault-local/`, `~/.claude/plugins/known_marketplaces.json`, `~/.claude/plugins/installed_plugins.json` | canary |
| `memory.Link` / `memory.Unlink` (`resolve()` when `opts.HomeDir==""`) | `vv memory link` / `vv memory unlink` | `~/.claude/projects/<slug>/memory` | sandboxed via `buildEnvWithHome` (`memory_link_cli`) |

### Category D — Canary helpers (read real HOME to monitor leaks)

`readOperatorConfigVaultPath`, `canaryHomePrivateSingleFiles`, and
`canaryHomePrivateRoots` in `test/integration_test.go` legitimately
call `os.UserHomeDir()` against the operator's real environment.
Their purpose is to detect when other subtests violate the sandbox.
Whitelisted via `//nolint:forbidigo // canary-real-home: ...` with
inline justification.

If a future canary helper needs real HOME, follow this same
nolint+justification pattern. Anything that does NOT need real HOME
must use `buildEnvWithHome` / `buildEnvWithHomeUser`.

### Env-builders (`test/integration_test.go:100–130`)

Three helpers live next to the `buildEnv` comment block — test authors
must pick one based on what the subtest reaches:

- **`buildEnv`** — vault-only subtests that do not reach any category-B
  read or category-C write. The real `$HOME` is passed through for
  stdlib compatibility (`user.Current`, etc.), but no operator-private
  path is touched.
- **`buildEnvWithHome`** — any subtest that invokes a category-B read
  or a category-C write. Currently used by `memory_link_cli`,
  `check_resume_invariants`, `vault_push_multi_remote`.
- **`buildEnvWithHomeUser`** — subtests that assert on provenance-
  stamped fields (`host` / `user` / `cwd` / `origin_project` in session
  notes or iteration trailers). Sets `VIBE_VAULT_HOSTNAME`,
  `VIBE_VAULT_CWD`, `USER`, `LOGNAME` to deterministic sentinels.
  Currently used by the provenance subtest.

### Canary coverage (`no_real_vault_mutation` subtest)

The canary snapshots several surfaces before any subtest runs and
re-snapshots after the last subtest, failing the run on any mutation:

- **Vault-rooted project subtrees** — `canaryProtectedRoots` enumerates
  the per-subtest project directories under the operator's real vault.
- **Operator config** — `~/.config/vibe-vault/config.toml`
  (`snap.configFile`).
- **Category-C home-private single files** (`snap.homeSingleFiles`):
  `~/.claude/settings.json`, `~/.config/zed/settings.json`,
  `~/.claude/plugins/known_marketplaces.json`,
  `~/.claude/plugins/installed_plugins.json`.
- **Category-C home-private directory tree** (`canaryHomePrivateRoots`):
  `~/.claude/plugins/cache/vibe-vault-local/` — scoped narrowly because
  Claude Code itself writes to other `~/.claude/plugins/` subtrees
  during normal operation.

A path that does not exist at pre-snapshot time is only flagged if the
test run *creates* it. An existing path is flagged on any
mtime-or-content mutation. If false positives appear on the narrowed
cache walk, add skip rules to `canaryShouldSkipFile` rather than
widening scope.

### `expandHome()` leak warning

`buildEnv` passes the real `$HOME` through to the subprocess. Any test
that writes a `~/...` string into a config value (e.g. `vault_path` in
a generated `config.toml`) resolves it against the operator's real
`$HOME` via `config/config.go`'s `expandHome`. No current test does
this, but a regression would leak writes outside the tempdir sandbox.
When in doubt, use `buildEnvWithHome` with a tempdir `HOME`.

(Originally audited via the `home-sandbox-audit` task — canary
regression gate established iter 136, extended for operator-private
write paths iter 141.)
