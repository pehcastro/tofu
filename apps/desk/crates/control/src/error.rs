use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

macro_rules! codes {
    ($($variant:ident $name:literal $code:literal,)*) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, JsonSchema)]
        pub enum ErrorCode {
            $(#[serde(rename = $name)] $variant,)*
        }

        impl ErrorCode {
            pub const ALL: &[ErrorCode] = &[$(ErrorCode::$variant,)*];

            pub fn rpc_code(self) -> i32 {
                match self {
                    $(ErrorCode::$variant => $code,)*
                }
            }

            pub fn name(self) -> &'static str {
                match self {
                    $(ErrorCode::$variant => $name,)*
                }
            }
        }
    };
}

codes! {
    ParseError "parse_error" -32700,
    InvalidRequest "invalid_request" -32600,
    UnknownMethod "unknown_method" -32601,
    InvalidParams "invalid_params" -32602,
    UnknownCommand "unknown_command" -32002,
    DeniedByConsent "denied_by_consent" -32003,
    DeniedByGate "denied_by_gate" -32004,
    NeedsApproval "needs_approval" -32005,
    Busy "busy" -32006,
    Timeout "timeout" -32007,
    Cancelled "cancelled" -32008,
    OverLimit "over_limit" -32009,
    PluginTrapped "plugin_trapped" -32010,
    PluginUnavailable "plugin_unavailable" -32011,
    NotFound "not_found" -32012,
    Conflict "conflict" -32013,
    UnsupportedVersion "unsupported_version" -32014,
    DeskNotRunning "desk_not_running" -32015,
    Refused "refused" -32016,
    TooMany "too_many" -32017,
    TooSmall "too_small" -32018,
    Locked "locked" -32019,
    NoSuchTile "no_such_tile" -32020,
    NoHistory "no_history" -32021,
    NothingClosed "nothing_closed" -32022,
    BadParams "bad_params" -32023,
}
