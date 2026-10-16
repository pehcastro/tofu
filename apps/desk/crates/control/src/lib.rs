mod envelope;
mod error;
pub mod events;
pub mod params;
pub mod replies;
pub mod shapes;
pub mod transport;
mod verbs;

pub use envelope::{
    ErrorData, Failure, Id, JsonRpc, Request, RpcError, Success, WireRequest, parse_line,
};
pub use error::ErrorCode;
pub use verbs::{Call, Method};

use schemars::SchemaGenerator;
use serde_json::{Map, Value, json};

pub const CONTRACT: &str = "desk.control/1";

pub fn schema() -> Value {
    let mut generator = SchemaGenerator::default();
    let requests: Map<String, Value> = Method::ALL
        .iter()
        .map(|method| (method.name().to_owned(), method.describe(&mut generator)))
        .collect();
    let errors: Vec<Value> = ErrorCode::ALL
        .iter()
        .map(|code| json!({"name": code.name(), "code": code.rpc_code()}))
        .collect();
    let envelope = json!({
        "request": generator.subschema_for::<WireRequest>(),
        "success": generator.subschema_for::<Success<Value>>(),
        "failure": generator.subschema_for::<Failure>(),
    });
    json!({
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "title": CONTRACT,
        "x-envelope": envelope,
        "x-requests": requests,
        "x-errors": errors,
        "$defs": generator.take_definitions(true),
    })
}
