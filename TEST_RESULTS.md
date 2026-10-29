# TEST_RESULTS

## Формат ведения

Новый прогон добавляется сверху, сразу под этим разделом. Формат заголовка: `## Прогон: YYYY-MM-DD HH:MM TZ (±HHMM)`. Результаты — только таблицей; после таблицы обязательны `### Сводка` и `### Падения`.

## Прогон: 2026-10-29 22:51 MSK (+0300)

Команда: `go test ./... -count=1 -race -v -timeout 600s`  
Go: `go1.27.1 linux/amd64`  
Ветка: `master`  

| # | Тест | Пакет | Результат | Время |
|---|------|-------|-----------|-------|
| 1 | TestIssueVerifyRoundTrip | internal/auth | PASS | 0.00s |
| 2 | TestTamperedTokenRejected | internal/auth | PASS | 0.00s |
| 3 | TestExpiredTokenRejected | internal/auth | PASS | 0.00s |
| 4 | TestIssuerValidation | internal/auth | PASS | 0.00s |
| 5 | TestCreateRingingCall | internal/call | PASS | 0.00s |
| 6 | TestCreateIsIdempotent | internal/call | PASS | 0.00s |
| 7 | TestRequestIDReuseIsRejected | internal/call | PASS | 0.00s |
| 8 | TestAcceptRequiresValidInvite | internal/call | PASS | 0.00s |
| 9 | TestCallerCannotAcceptOwnCall | internal/call | PASS | 0.00s |
| 10 | TestAcceptIsIdempotent | internal/call | PASS | 0.00s |
| 11 | TestRejectOnlyByCallee | internal/call | PASS | 0.00s |
| 12 | TestCancelOnlyByCallerWhileRinging | internal/call | PASS | 0.00s |
| 13 | TestEndIsIdempotentAndTerminal | internal/call | PASS | 0.00s |
| 14 | TestConcurrentHangup | internal/call | PASS | 0.00s |
| 15 | TestConnectedRequiresBothReports | internal/call | PASS | 0.00s |
| 16 | TestSdpNegotiationDoesNotConnect | internal/call | PASS | 0.00s |
| 17 | TestInvitationTimeout | internal/call | PASS | 0.00s |
| 18 | TestConnectionTimeout | internal/call | PASS | 0.00s |
| 19 | TestInviteExpiryIsEnforced | internal/call | PASS | 0.00s |
| 20 | TestMembershipIsAuthorized | internal/call | PASS | 0.00s |
| 21 | TestOneActiveCallPerParticipant | internal/call | PASS | 0.00s |
| 22 | TestDirectCallRequiresOnlineCallee | internal/call | PASS | 0.00s |
| 23 | TestRetentionRemovesTerminalCalls | internal/call | PASS | 0.00s |
| 24 | TestConcurrentCreateAcceptEnd | internal/call | PASS | 0.00s |
| 25 | TestConfigValidation | internal/call | PASS | 0.00s |
| 26 | TestNormalizeServer | internal/client | PASS | 0.00s |
| 27 | TestRegister | internal/client | PASS | 0.00s |
| 28 | TestRegisterErrors | internal/client | PASS | 0.00s |
| 29 | TestLoadMissingFileIsNotAnError | internal/config | PASS | 0.00s |
| 30 | TestSaveThenLoadRoundTrip | internal/config | PASS | 0.00s |
| 31 | TestSaveThenLoadServer | internal/config | PASS | 0.00s |
| 32 | TestSaveIsAtomicAndOverwrites | internal/config | PASS | 0.00s |
| 33 | TestLoadIgnoresCommentsAndUnknownKeys | internal/config | PASS | 0.00s |
| 34 | TestLoadRejectsMalformedLine | internal/config | PASS | 0.00s |
| 35 | TestSaveCreatesParentDirs | internal/config | PASS | 0.00s |
| 36 | TestValidateName | internal/config | PASS | 0.00s |
| 37 | TestValidateNameEmptyReportsErrNoName | internal/config | PASS | 0.00s |
| 38 | TestDefaultPathUsesWormHome | internal/config | PASS | 0.00s |
| 39 | TestDefaultPathIsIndependentOfWorkingDir | internal/config | PASS | 0.00s |
| 40 | TestEndToEndCallWithMedia | internal/engine | SKIP | 0.00s |
| 41 | TestCallRejectNotifiesCaller | internal/engine | PASS | 0.03s |
| 42 | TestCallerCancelNotifiesCallee | internal/engine | PASS | 0.04s |
| 43 | TestUnauthorizedCallIsRejectedByServer | internal/engine | PASS | 0.02s |
| 44 | TestEngineRequiresConnection | internal/engine | PASS | 0.00s |
| 45 | TestDirDefaultsToHome | internal/home | PASS | 0.00s |
| 46 | TestWormHomeOverride | internal/home | PASS | 0.00s |
| 47 | TestEnsureCreatesLayout | internal/home | PASS | 0.00s |
| 48 | TestEnsureIsIdempotentAndPreservesUserEdits | internal/home | PASS | 0.00s |
| 49 | TestLoadAssetInstallsDefault | internal/home | PASS | 0.00s |
| 50 | TestLoadAssetReturnsUserFile | internal/home | PASS | 0.00s |
| 51 | TestPathsMatchLayout | internal/home | PASS | 0.00s |
| 52 | TestEncodeDecodeRoundTrip | internal/protocol | PASS | 0.00s |
| 53 | TestDecodeRejectsUnknownFields | internal/protocol | PASS | 0.00s |
| 54 | TestDecodeRejectsBadVersion | internal/protocol | PASS | 0.00s |
| 55 | TestDecodeRejectsUnknownType | internal/protocol | PASS | 0.00s |
| 56 | TestDecodeRequiresCallIDForCallScopedTypes | internal/protocol | PASS | 0.00s |
| 57 | TestDecodeRejectsOversizedMessage | internal/protocol | PASS | 0.00s |
| 58 | TestPayloadValidation | internal/protocol | PASS | 0.00s |
| 59 | TestValidateName | internal/protocol | PASS | 0.00s |
| 60 | TestValidateReason | internal/protocol | PASS | 0.00s |
| 61 | TestErrorPayloadPreservesRequestID | internal/protocol | PASS | 0.00s |
| 62 | TestDecodeRejectsInvalidRequestIDCharacters | internal/protocol | PASS | 0.00s |
| 63 | TestCallStateDeadlineEncoding | internal/protocol | PASS | 0.00s |
| 64 | TestSessionEndpointIssuesTokenAndIceServers | internal/server | PASS | 0.00s |
| 65 | TestSessionEndpointRejectsInvalidName | internal/server | PASS | 0.00s |
| 66 | TestSessionRateLimit | internal/server | PASS | 0.00s |
| 67 | TestTurnCredentialsEndpoint | internal/server | PASS | 0.00s |
| 68 | TestTurnDisabled | internal/server | PASS | 0.00s |
| 69 | TestMetricsEndpoint | internal/server | PASS | 0.00s |
| 70 | TestHealthAndReady | internal/server | PASS | 0.00s |
| 71 | TestWSRequiresToken | internal/server | PASS | 0.00s |
| 72 | TestPublicURLUsedForWS | internal/server | PASS | 0.00s |
| 73 | TestSessionResponseIsNotCached | internal/server | PASS | 0.00s |
| 74 | TestStartAndShutdown | internal/server | PASS | 0.00s |
| 75 | TestLoadConfigDefaults | internal/server | PASS | 0.00s |
| 76 | TestLoadConfigRequiresSecret | internal/server | PASS | 0.00s |
| 77 | TestLoadConfigParsesEnvironment | internal/server | PASS | 0.00s |
| 78 | TestLoadConfigRejectsInvalidDuration | internal/server | PASS | 0.00s |
| 79 | TestValidateRejectsBadPublicURL | internal/server | PASS | 0.00s |
| 80 | TestInviteRoundTrip | internal/session | PASS | 0.00s |
| 81 | TestInviteIgnoresWhitespace | internal/session | PASS | 0.00s |
| 82 | TestInviteRejectsGarbage | internal/session | PASS | 0.00s |
| 83 | TestInviteValidation | internal/session | PASS | 0.00s |
| 84 | TestGroup | internal/session | PASS | 0.00s |
| 85 | TestRandomID | internal/session | PASS | 0.00s |
| 86 | TestUnauthorizedConnectionRejected | internal/signaling | PASS | 0.00s |
| 87 | TestCreateCallReturnsRingingState | internal/signaling | PASS | 0.01s |
| 88 | TestFullSignalingFlow | internal/signaling | PASS | 0.01s |
| 89 | TestSignalingRequiresMembership | internal/signaling | PASS | 0.01s |
| 90 | TestDuplicateCreateIsIdempotent | internal/signaling | PASS | 0.00s |
| 91 | TestDuplicateOfferForwardedOnce | internal/signaling | PASS | 0.16s |
| 92 | TestInvalidMessageTypeRejected | internal/signaling | PASS | 0.00s |
| 93 | TestPingPong | internal/signaling | PASS | 0.00s |
| 94 | TestCandidatesAreBufferedWhilePeerIsOffline | internal/signaling | PASS | 0.11s |
| 95 | TestReconnectGraceFailsCall | internal/signaling | PASS | 0.31s |
| 96 | TestRateLimitReturnsError | internal/signaling | PASS | 0.00s |
| 97 | TestMalformedMessageReturnsError | internal/signaling | PASS | 0.00s |
| 98 | TestRecoverReturnsCurrentState | internal/signaling | PASS | 0.00s |
| 99 | TestMainScreenShowsActionsAndServer | internal/tui | PASS | 0.01s |
| 100 | TestCreateFlowStartsEngine | internal/tui | PASS | 0.00s |
| 101 | TestJoinFlowAcceptsIncomingCall | internal/tui | PASS | 0.02s |
| 102 | TestJoinRejectsBadInvite | internal/tui | PASS | 0.00s |
| 103 | TestMissingServerIsReported | internal/tui | PASS | 0.01s |
| 104 | TestRegisterFailureIsShown | internal/tui | PASS | 0.00s |
| 105 | TestMuteAndLeave | internal/tui | PASS | 0.00s |
| 106 | TestLeaveWhileRingingCancels | internal/tui | PASS | 0.00s |
| 107 | TestRejectFromIncomingCall | internal/tui | PASS | 0.00s |
| 108 | TestTerminalStateClosesEngine | internal/tui | PASS | 0.00s |
| 109 | TestEngineErrorIsShown | internal/tui | PASS | 0.00s |
| 110 | TestCallScreenShowsPanels | internal/tui | PASS | 0.00s |
| 111 | TestRenamePersistsToConfig | internal/tui | PASS | 0.00s |
| 112 | TestRenameRejectsEmptyName | internal/tui | PASS | 0.00s |
| 113 | TestWindowSizePropagates | internal/tui | PASS | 0.00s |
| 114 | TestIssueCredentials | internal/turnserver | PASS | 0.00s |
| 115 | TestAuthHandlerAcceptsValidUsername | internal/turnserver | PASS | 0.00s |
| 116 | TestAuthHandlerRejectsExpiredAndMalformed | internal/turnserver | PASS | 0.00s |
| 117 | TestServerAllocatesRelay | internal/turnserver | PASS | 0.01s |
| 118 | TestServerRejectsBadPassword | internal/turnserver | PASS | 0.01s |
| 119 | TestStartRejectsBadSecret | internal/turnserver | PASS | 0.00s |

### Сводка

- Всего: 119
- PASS: 118
- FAIL: 0
- SKIP: 1
- Время: суммарное время пакетов ≈ 0.76s

### Падения

нет

Пропуск: `TestEndToEndCallWithMedia` под `-race` — DTLS-хендшейк под инструментацией не укладывается в ICE-таймеры; медиа-путь покрыт прогоном без `-race`.

## Прогон: 2026-10-29 22:51 MSK (+0300)

Команда: `go test ./... -count=1 -v -timeout 300s`  
Go: `go1.27.1 linux/amd64`  
Ветка: `master`  

| # | Тест | Пакет | Результат | Время |
|---|------|-------|-----------|-------|
| 1 | TestIssueVerifyRoundTrip | internal/auth | PASS | 0.00s |
| 2 | TestTamperedTokenRejected | internal/auth | PASS | 0.00s |
| 3 | TestExpiredTokenRejected | internal/auth | PASS | 0.00s |
| 4 | TestIssuerValidation | internal/auth | PASS | 0.00s |
| 5 | TestCreateRingingCall | internal/call | PASS | 0.00s |
| 6 | TestCreateIsIdempotent | internal/call | PASS | 0.00s |
| 7 | TestRequestIDReuseIsRejected | internal/call | PASS | 0.00s |
| 8 | TestAcceptRequiresValidInvite | internal/call | PASS | 0.00s |
| 9 | TestCallerCannotAcceptOwnCall | internal/call | PASS | 0.00s |
| 10 | TestAcceptIsIdempotent | internal/call | PASS | 0.00s |
| 11 | TestRejectOnlyByCallee | internal/call | PASS | 0.00s |
| 12 | TestCancelOnlyByCallerWhileRinging | internal/call | PASS | 0.00s |
| 13 | TestEndIsIdempotentAndTerminal | internal/call | PASS | 0.00s |
| 14 | TestConcurrentHangup | internal/call | PASS | 0.00s |
| 15 | TestConnectedRequiresBothReports | internal/call | PASS | 0.00s |
| 16 | TestSdpNegotiationDoesNotConnect | internal/call | PASS | 0.00s |
| 17 | TestInvitationTimeout | internal/call | PASS | 0.00s |
| 18 | TestConnectionTimeout | internal/call | PASS | 0.00s |
| 19 | TestInviteExpiryIsEnforced | internal/call | PASS | 0.00s |
| 20 | TestMembershipIsAuthorized | internal/call | PASS | 0.00s |
| 21 | TestOneActiveCallPerParticipant | internal/call | PASS | 0.00s |
| 22 | TestDirectCallRequiresOnlineCallee | internal/call | PASS | 0.00s |
| 23 | TestRetentionRemovesTerminalCalls | internal/call | PASS | 0.00s |
| 24 | TestConcurrentCreateAcceptEnd | internal/call | PASS | 0.00s |
| 25 | TestConfigValidation | internal/call | PASS | 0.00s |
| 26 | TestNormalizeServer | internal/client | PASS | 0.00s |
| 27 | TestRegister | internal/client | PASS | 0.00s |
| 28 | TestRegisterErrors | internal/client | PASS | 0.00s |
| 29 | TestLoadMissingFileIsNotAnError | internal/config | PASS | 0.00s |
| 30 | TestSaveThenLoadRoundTrip | internal/config | PASS | 0.00s |
| 31 | TestSaveThenLoadServer | internal/config | PASS | 0.00s |
| 32 | TestSaveIsAtomicAndOverwrites | internal/config | PASS | 0.00s |
| 33 | TestLoadIgnoresCommentsAndUnknownKeys | internal/config | PASS | 0.00s |
| 34 | TestLoadRejectsMalformedLine | internal/config | PASS | 0.00s |
| 35 | TestSaveCreatesParentDirs | internal/config | PASS | 0.00s |
| 36 | TestValidateName | internal/config | PASS | 0.00s |
| 37 | TestValidateNameEmptyReportsErrNoName | internal/config | PASS | 0.00s |
| 38 | TestDefaultPathUsesWormHome | internal/config | PASS | 0.00s |
| 39 | TestDefaultPathIsIndependentOfWorkingDir | internal/config | PASS | 0.00s |
| 40 | TestEndToEndCallWithMedia | internal/engine | PASS | 1.05s |
| 41 | TestCallRejectNotifiesCaller | internal/engine | PASS | 0.02s |
| 42 | TestCallerCancelNotifiesCallee | internal/engine | PASS | 0.03s |
| 43 | TestUnauthorizedCallIsRejectedByServer | internal/engine | PASS | 0.02s |
| 44 | TestEngineRequiresConnection | internal/engine | PASS | 0.00s |
| 45 | TestDirDefaultsToHome | internal/home | PASS | 0.00s |
| 46 | TestWormHomeOverride | internal/home | PASS | 0.00s |
| 47 | TestEnsureCreatesLayout | internal/home | PASS | 0.00s |
| 48 | TestEnsureIsIdempotentAndPreservesUserEdits | internal/home | PASS | 0.00s |
| 49 | TestLoadAssetInstallsDefault | internal/home | PASS | 0.00s |
| 50 | TestLoadAssetReturnsUserFile | internal/home | PASS | 0.00s |
| 51 | TestPathsMatchLayout | internal/home | PASS | 0.00s |
| 52 | TestEncodeDecodeRoundTrip | internal/protocol | PASS | 0.00s |
| 53 | TestDecodeRejectsUnknownFields | internal/protocol | PASS | 0.00s |
| 54 | TestDecodeRejectsBadVersion | internal/protocol | PASS | 0.00s |
| 55 | TestDecodeRejectsUnknownType | internal/protocol | PASS | 0.00s |
| 56 | TestDecodeRequiresCallIDForCallScopedTypes | internal/protocol | PASS | 0.00s |
| 57 | TestDecodeRejectsOversizedMessage | internal/protocol | PASS | 0.00s |
| 58 | TestPayloadValidation | internal/protocol | PASS | 0.00s |
| 59 | TestValidateName | internal/protocol | PASS | 0.00s |
| 60 | TestValidateReason | internal/protocol | PASS | 0.00s |
| 61 | TestErrorPayloadPreservesRequestID | internal/protocol | PASS | 0.00s |
| 62 | TestDecodeRejectsInvalidRequestIDCharacters | internal/protocol | PASS | 0.00s |
| 63 | TestCallStateDeadlineEncoding | internal/protocol | PASS | 0.00s |
| 64 | TestSessionEndpointIssuesTokenAndIceServers | internal/server | PASS | 0.00s |
| 65 | TestSessionEndpointRejectsInvalidName | internal/server | PASS | 0.00s |
| 66 | TestSessionRateLimit | internal/server | PASS | 0.00s |
| 67 | TestTurnCredentialsEndpoint | internal/server | PASS | 0.00s |
| 68 | TestTurnDisabled | internal/server | PASS | 0.00s |
| 69 | TestMetricsEndpoint | internal/server | PASS | 0.00s |
| 70 | TestHealthAndReady | internal/server | PASS | 0.00s |
| 71 | TestWSRequiresToken | internal/server | PASS | 0.00s |
| 72 | TestPublicURLUsedForWS | internal/server | PASS | 0.00s |
| 73 | TestSessionResponseIsNotCached | internal/server | PASS | 0.00s |
| 74 | TestStartAndShutdown | internal/server | PASS | 0.00s |
| 75 | TestLoadConfigDefaults | internal/server | PASS | 0.00s |
| 76 | TestLoadConfigRequiresSecret | internal/server | PASS | 0.00s |
| 77 | TestLoadConfigParsesEnvironment | internal/server | PASS | 0.00s |
| 78 | TestLoadConfigRejectsInvalidDuration | internal/server | PASS | 0.00s |
| 79 | TestValidateRejectsBadPublicURL | internal/server | PASS | 0.00s |
| 80 | TestInviteRoundTrip | internal/session | PASS | 0.00s |
| 81 | TestInviteIgnoresWhitespace | internal/session | PASS | 0.00s |
| 82 | TestInviteRejectsGarbage | internal/session | PASS | 0.00s |
| 83 | TestInviteValidation | internal/session | PASS | 0.00s |
| 84 | TestGroup | internal/session | PASS | 0.00s |
| 85 | TestRandomID | internal/session | PASS | 0.00s |
| 86 | TestUnauthorizedConnectionRejected | internal/signaling | PASS | 0.00s |
| 87 | TestCreateCallReturnsRingingState | internal/signaling | PASS | 0.00s |
| 88 | TestFullSignalingFlow | internal/signaling | PASS | 0.00s |
| 89 | TestSignalingRequiresMembership | internal/signaling | PASS | 0.00s |
| 90 | TestDuplicateCreateIsIdempotent | internal/signaling | PASS | 0.00s |
| 91 | TestDuplicateOfferForwardedOnce | internal/signaling | PASS | 0.15s |
| 92 | TestInvalidMessageTypeRejected | internal/signaling | PASS | 0.00s |
| 93 | TestPingPong | internal/signaling | PASS | 0.00s |
| 94 | TestCandidatesAreBufferedWhilePeerIsOffline | internal/signaling | PASS | 0.10s |
| 95 | TestReconnectGraceFailsCall | internal/signaling | PASS | 0.30s |
| 96 | TestRateLimitReturnsError | internal/signaling | PASS | 0.00s |
| 97 | TestMalformedMessageReturnsError | internal/signaling | PASS | 0.00s |
| 98 | TestRecoverReturnsCurrentState | internal/signaling | PASS | 0.00s |
| 99 | TestMainScreenShowsActionsAndServer | internal/tui | PASS | 0.00s |
| 100 | TestCreateFlowStartsEngine | internal/tui | PASS | 0.00s |
| 101 | TestJoinFlowAcceptsIncomingCall | internal/tui | PASS | 0.00s |
| 102 | TestJoinRejectsBadInvite | internal/tui | PASS | 0.00s |
| 103 | TestMissingServerIsReported | internal/tui | PASS | 0.00s |
| 104 | TestRegisterFailureIsShown | internal/tui | PASS | 0.00s |
| 105 | TestMuteAndLeave | internal/tui | PASS | 0.00s |
| 106 | TestLeaveWhileRingingCancels | internal/tui | PASS | 0.00s |
| 107 | TestRejectFromIncomingCall | internal/tui | PASS | 0.00s |
| 108 | TestTerminalStateClosesEngine | internal/tui | PASS | 0.00s |
| 109 | TestEngineErrorIsShown | internal/tui | PASS | 0.00s |
| 110 | TestCallScreenShowsPanels | internal/tui | PASS | 0.00s |
| 111 | TestRenamePersistsToConfig | internal/tui | PASS | 0.00s |
| 112 | TestRenameRejectsEmptyName | internal/tui | PASS | 0.00s |
| 113 | TestWindowSizePropagates | internal/tui | PASS | 0.00s |
| 114 | TestIssueCredentials | internal/turnserver | PASS | 0.00s |
| 115 | TestAuthHandlerAcceptsValidUsername | internal/turnserver | PASS | 0.00s |
| 116 | TestAuthHandlerRejectsExpiredAndMalformed | internal/turnserver | PASS | 0.00s |
| 117 | TestServerAllocatesRelay | internal/turnserver | PASS | 0.00s |
| 118 | TestServerRejectsBadPassword | internal/turnserver | PASS | 0.00s |
| 119 | TestStartRejectsBadSecret | internal/turnserver | PASS | 0.00s |

### Сводка

- Всего: 119
- PASS: 119
- FAIL: 0
- SKIP: 0
- Время: суммарное время пакетов ≈ 1.67s

### Падения

нет
