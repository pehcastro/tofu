use std::alloc::System;

use serde::Serialize;
use stats_alloc::StatsAlloc;

pub type CountingAllocator = StatsAlloc<System>;

pub static ALLOCATOR: CountingAllocator = StatsAlloc::system();

#[derive(Clone, Copy, Debug, Default, Serialize)]
pub struct MemorySample {
    pub working_set: usize,
    pub private_bytes: usize,
}

pub(crate) fn allocations() -> usize {
    let stats = ALLOCATOR.stats();
    stats.allocations.saturating_add(stats.reallocations)
}

pub(crate) fn sample() -> Option<MemorySample> {
    memory_stats::memory_stats().map(|usage| MemorySample {
        working_set: usage.physical_mem,
        private_bytes: usage.virtual_mem,
    })
}
