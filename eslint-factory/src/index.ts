import { noCoreExportVariableNonStringRule } from "./rules/no-core-exportvariable-non-string";
import { noCoreSetOutputNonStringRule } from "./rules/no-core-setoutput-non-string";
import { noThrowPlainObjectRule } from "./rules/no-throw-plain-object";
import { noGithubRequestInterpolatedRouteRule } from "./rules/no-github-request-interpolated-route";
import { noJsonStringifyErrorRule } from "./rules/no-json-stringify-error";
import { noJsonStringifyEqualityRule } from "./rules/no-json-stringify-equality";
import { noJsonStringifySetOrMapRule } from "./rules/no-json-stringify-set-or-map";
import { noUnsafeCatchErrorPropertyRule } from "./rules/no-unsafe-catch-error-property";
import { noUnsafePromiseCatchErrorPropertyRule } from "./rules/no-unsafe-promise-catch-error-property";
import { preferGetErrorMessageRule } from "./rules/prefer-get-error-message";
import { preferGetErrorMessageOverStringRule } from "./rules/prefer-get-error-message-over-string";
import { preferNumberIsNanRule } from "./rules/prefer-number-isnan";
import { requireAsyncEntrypointCatchRule } from "./rules/require-async-entrypoint-catch";
import { requireAwaitCoreSummaryWriteRule } from "./rules/require-await-core-summary-write";
import { requireFsCloseSyncRule } from "./rules/require-fs-close-sync";
import { requireFsSyncTryCatchRule } from "./rules/require-fs-sync-try-catch";
import { requireJsonParseTryCatchRule } from "./rules/require-json-parse-try-catch";
import { requireErrorCauseInRethrowRule } from "./rules/require-error-cause-in-rethrow";
import { requireParseIntRadixRule } from "./rules/require-parseInt-radix";
import { requireMkdirSyncTryCatchRule } from "./rules/require-mkdirsync-try-catch";
import { requireMkdtempSyncTryCatchRule } from "./rules/require-mkdtempsync-try-catch";
import { requireRealpathSyncTryCatchRule } from "./rules/require-realpathsync-try-catch";
import { requireRmSyncTryCatchRule } from "./rules/require-rmsync-try-catch";
import { requireReturnAfterCoreSetFailedRule } from "./rules/require-return-after-core-setfailed";
import { requireSpawnSyncErrorCheckRule } from "./rules/require-spawnsync-error-check";
import { requireSpawnErrorListenerRule } from "./rules/require-spawn-error-listener";
import { requireDecodeURIComponentTryCatchRule } from "./rules/require-decodeuricomponent-try-catch";
import { requireNewUrlTryCatchRule } from "./rules/require-new-url-try-catch";
import { preferCoreLoggingRule } from "./rules/prefer-core-logging";
import { noCoreErrorThenProcessExitRule } from "./rules/no-core-error-then-process-exit";
import { noCoreErrorThenProcessExitCodeRule } from "./rules/no-core-error-then-process-exitcode";
import { noChildProcessInterpolatedCommandRule } from "./rules/no-child-process-interpolated-command";
import { noExecInterpolatedCommandRule } from "./rules/no-exec-interpolated-command";
import { requireExecSyncTryCatchRule } from "./rules/require-execsync-try-catch";
import { requireExecFileSyncTryCatchRule } from "./rules/require-execfilesync-try-catch";
import { requireFsIoTryCatchRule } from "./rules/require-fs-io-try-catch";
import { noSetFailedThenExitZeroRule } from "./rules/no-setfailed-then-exit-zero";
import { noErrStackThenStringFallbackRule } from "./rules/no-err-stack-then-string-fallback";
import { noCaughtErrorInterpolationRule } from "./rules/no-caught-error-interpolation";
import { requireFetchTryCatchRule } from "./rules/require-fetch-try-catch";
import { noCoreErrorThenSetFailedRule } from "./rules/no-core-error-then-setfailed";
import { noDuplicateConstantValuesRule } from "./rules/no-duplicate-constant-values";
import { requireEscapedRegexpInterpolationRule } from "./rules/require-escaped-regexp-interpolation";
import { requireFetchTimeoutRule } from "./rules/require-fetch-timeout";
import { requireNanCheckAfterEnvNumericParseRule } from "./rules/require-nan-check-after-env-numeric-parse";
import { requireNanCheckAfterSplitIndexParseRule } from "./rules/require-nan-check-after-split-index-parse";
import { preferStructuredCloneRule } from "./rules/prefer-structured-clone";
import { requireFetchResponseBodyTryCatchRule } from "./rules/require-fetch-response-body-try-catch";
import { requireErrorCodeInThrownErrorRule } from "./rules/require-error-code-in-thrown-error";
import { requireInvalidDateCheckBeforeCompareRule } from "./rules/require-invalid-date-check-before-compare";
import { requireSyncExecTimeoutRule } from "./rules/require-sync-exec-timeout";
import { noEmptyCatchBlockRule } from "./rules/no-empty-catch-block";
import { requireLastIndexResetBeforeGlobalExecLoopRule } from "./rules/require-lastindex-reset-before-global-exec-loop";
import { requirePageCounterIncrementInWhileTrueLoopRule } from "./rules/require-page-counter-increment-in-while-true-loop";
import { noMathMinMaxArraySpreadRule } from "./rules/no-math-minmax-array-spread";
import { requireErrorCodeForGithubApiThrowRule } from "./rules/require-error-code-for-github-api-throw";
import { requireHttpResponseErrorListenerRule } from "./rules/require-http-response-error-listener";
import { noStringFallbackForNonStringMessageRule } from "./rules/no-string-fallback-for-non-string-message";
import { requireGetExecOutputExitCodeCheckRule } from "./rules/require-getexecoutput-exitcode-check";
import { preferActionsExecOverChildProcessRule } from "./rules/prefer-actions-exec-over-child-process";
import { noMisplacedErrorCodeDefinitionRule } from "./rules/no-misplaced-error-code-definition";
import { requireFsChmodTryCatchRule } from "./rules/require-fs-chmod-try-catch";

const plugin = {
  meta: {
    name: "@github/gh-aw-eslint-factory",
    version: "0.1.0",
  },
  rules: {
    "no-core-exportvariable-non-string": noCoreExportVariableNonStringRule,
    "no-core-setoutput-non-string": noCoreSetOutputNonStringRule,
    "no-throw-plain-object": noThrowPlainObjectRule,
    "no-github-request-interpolated-route": noGithubRequestInterpolatedRouteRule,
    "no-json-stringify-error": noJsonStringifyErrorRule,
    "no-json-stringify-equality": noJsonStringifyEqualityRule,
    "no-json-stringify-set-or-map": noJsonStringifySetOrMapRule,
    "no-unsafe-catch-error-property": noUnsafeCatchErrorPropertyRule,
    "no-unsafe-promise-catch-error-property": noUnsafePromiseCatchErrorPropertyRule,
    "prefer-get-error-message": preferGetErrorMessageRule,
    "prefer-get-error-message-over-string": preferGetErrorMessageOverStringRule,
    "prefer-number-isnan": preferNumberIsNanRule,
    "require-async-entrypoint-catch": requireAsyncEntrypointCatchRule,
    "require-await-core-summary-write": requireAwaitCoreSummaryWriteRule,
    "require-fs-close-sync": requireFsCloseSyncRule,
    "require-error-cause-in-rethrow": requireErrorCauseInRethrowRule,
    "require-fs-sync-try-catch": requireFsSyncTryCatchRule,
    "require-json-parse-try-catch": requireJsonParseTryCatchRule,
    "require-mkdirsync-try-catch": requireMkdirSyncTryCatchRule,
    "require-mkdtempsync-try-catch": requireMkdtempSyncTryCatchRule,
    "require-realpathsync-try-catch": requireRealpathSyncTryCatchRule,
    "require-rmsync-try-catch": requireRmSyncTryCatchRule,
    "require-parseInt-radix": requireParseIntRadixRule,
    "require-return-after-core-setfailed": requireReturnAfterCoreSetFailedRule,
    "require-spawnsync-error-check": requireSpawnSyncErrorCheckRule,
    "require-spawn-error-listener": requireSpawnErrorListenerRule,
    "require-new-url-try-catch": requireNewUrlTryCatchRule,
    "require-decodeuricomponent-try-catch": requireDecodeURIComponentTryCatchRule,
    "prefer-core-logging": preferCoreLoggingRule,
    "no-core-error-then-process-exit": noCoreErrorThenProcessExitRule,
    "no-core-error-then-process-exitcode": noCoreErrorThenProcessExitCodeRule,
    "no-child-process-interpolated-command": noChildProcessInterpolatedCommandRule,
    "no-exec-interpolated-command": noExecInterpolatedCommandRule,
    "require-execsync-try-catch": requireExecSyncTryCatchRule,
    "require-execfilesync-try-catch": requireExecFileSyncTryCatchRule,
    "require-fs-io-try-catch": requireFsIoTryCatchRule,
    "no-setfailed-then-exit-zero": noSetFailedThenExitZeroRule,
    "no-err-stack-then-string-fallback": noErrStackThenStringFallbackRule,
    "no-caught-error-interpolation": noCaughtErrorInterpolationRule,
    "require-fetch-try-catch": requireFetchTryCatchRule,
    "no-core-error-then-setfailed": noCoreErrorThenSetFailedRule,
    "no-duplicate-constant-values": noDuplicateConstantValuesRule,
    "require-escaped-regexp-interpolation": requireEscapedRegexpInterpolationRule,
    "require-fetch-timeout": requireFetchTimeoutRule,
    "require-nan-check-after-env-numeric-parse": requireNanCheckAfterEnvNumericParseRule,
    "require-nan-check-after-split-index-parse": requireNanCheckAfterSplitIndexParseRule,
    "prefer-structured-clone": preferStructuredCloneRule,
    "require-fetch-response-body-try-catch": requireFetchResponseBodyTryCatchRule,
    "require-error-code-in-thrown-error": requireErrorCodeInThrownErrorRule,
    "require-invalid-date-check-before-compare": requireInvalidDateCheckBeforeCompareRule,
    "require-sync-exec-timeout": requireSyncExecTimeoutRule,
    "no-empty-catch-block": noEmptyCatchBlockRule,
    "require-lastindex-reset-before-global-exec-loop": requireLastIndexResetBeforeGlobalExecLoopRule,
    "require-page-counter-increment-in-while-true-loop": requirePageCounterIncrementInWhileTrueLoopRule,
    "no-math-minmax-array-spread": noMathMinMaxArraySpreadRule,
    "require-error-code-for-github-api-throw": requireErrorCodeForGithubApiThrowRule,
    "require-http-response-error-listener": requireHttpResponseErrorListenerRule,
    "no-string-fallback-for-non-string-message": noStringFallbackForNonStringMessageRule,
    "require-getexecoutput-exitcode-check": requireGetExecOutputExitCodeCheckRule,
    "prefer-actions-exec-over-child-process": preferActionsExecOverChildProcessRule,
    "no-misplaced-error-code-definition": noMisplacedErrorCodeDefinitionRule,
    "require-fs-chmod-try-catch": requireFsChmodTryCatchRule,
  },
};

export = plugin;
