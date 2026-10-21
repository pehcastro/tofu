mod bench;
mod capture;
#[cfg(feature = "profiler")]
mod cells;
mod frames;
mod grab;
pub mod limits;
mod memory;
#[cfg(feature = "profiler")]
mod overlay;
#[cfg(feature = "profiler")]
mod probe;
#[cfg(feature = "profiler")]
mod profiler;
#[cfg(feature = "profiler")]
mod record;
#[cfg(feature = "profiler")]
mod span;
#[cfg(feature = "profiler")]
mod trace;

#[cfg(feature = "profiler")]
#[macro_export]
macro_rules! profiled {
    (on { $($on:tt)* } off { $($off:tt)* }) => { $($on)* };
}

#[cfg(not(feature = "profiler"))]
#[macro_export]
macro_rules! profiled {
    (on { $($on:tt)* } off { $($off:tt)* }) => { $($off)* };
}

#[cfg(not(feature = "profiler"))]
#[macro_export]
macro_rules! span {
    ($name:expr) => {
        let _: &'static str = $name;
    };
}

pub use bench::{BenchResult, run_bench};
pub use capture::CaptureError;
pub use frames::FrameSummary;
pub use grab::{GrabError, grab, grab_region};
pub use memory::{ALLOCATOR, CountingAllocator, MemorySample};
#[cfg(feature = "profiler")]
pub use overlay::overlay;
#[cfg(feature = "profiler")]
pub use profiler::Profiler;
#[cfg(feature = "profiler")]
pub use span::{Cascade, CascadeId, Span};
