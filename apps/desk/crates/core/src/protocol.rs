use serde::de::DeserializeOwned;
use serde::{Deserialize, Deserializer, Serialize};

include!(concat!(env!("OUT_DIR"), "/protocol.rs"));

pub trait Request {
    const METHOD: &'static str;
    type Params: Serialize;
    type Result: DeserializeOwned;
}

#[derive(Clone, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(untagged)]
pub enum RequestId {
    Number(i64),
    Text(String),
}

fn null_as_empty<'de, D: Deserializer<'de>, T: Deserialize<'de>>(
    deserializer: D,
) -> Result<Vec<T>, D::Error> {
    Ok(Option::<Vec<T>>::deserialize(deserializer)?.unwrap_or_default())
}
