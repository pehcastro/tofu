use std::backtrace::Backtrace;
use std::cell::Cell;
use std::mem;
use std::sync::atomic::{AtomicU32, AtomicU64, Ordering};
use std::sync::{Arc, Mutex, PoisonError};
use std::time::Instant;

use crate::limits::{HANG_THRESHOLD, PENDING_SPANS};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub struct CascadeId(u64);

impl CascadeId {
    pub(crate) fn raw(self) -> u64 {
        self.0
    }
}

#[derive(Clone, Debug)]
pub struct SpanRecord {
    pub name: &'static str,
    pub cascade: Option<CascadeId>,
    pub thread: u32,
    pub depth: u16,
    pub start: Instant,
    pub end: Instant,
    pub stack: Option<Arc<str>>,
}

#[derive(Clone, Copy)]
struct ThreadState {
    thread: u32,
    depth: u16,
    cascade: Option<CascadeId>,
}

struct Pending {
    spans: Vec<SpanRecord>,
    dropped: u64,
}

static NEXT_THREAD: AtomicU32 = AtomicU32::new(1);
static NEXT_CASCADE: AtomicU64 = AtomicU64::new(1);
static PENDING: Mutex<Pending> = Mutex::new(Pending {
    spans: Vec::new(),
    dropped: 0,
});

thread_local! {
    static THREAD: Cell<ThreadState> = Cell::new(ThreadState {
        thread: NEXT_THREAD.fetch_add(1, Ordering::Relaxed),
        depth: 0,
        cascade: None,
    });
}

#[macro_export]
macro_rules! span {
    ($name:expr) => {
        let _perf_span = $crate::Span::open($name);
    };
}

#[must_use]
pub struct Span(Option<(&'static str, ThreadState, Instant)>);

impl Span {
    #[inline]
    pub fn open(name: &'static str) -> Self {
        if !gpui::trace_enabled() {
            return Span(None);
        }
        let state = THREAD.with(|thread| {
            let state = thread.get();
            thread.set(ThreadState {
                depth: state.depth.saturating_add(1),
                ..state
            });
            state
        });
        Span(Some((name, state, Instant::now())))
    }
}

impl Drop for Span {
    fn drop(&mut self) {
        let Some((name, state, start)) = self.0.take() else {
            return;
        };
        let end = Instant::now();
        THREAD.with(|thread| {
            thread.set(ThreadState {
                depth: state.depth,
                ..thread.get()
            })
        });
        let stack = (end.duration_since(start) >= HANG_THRESHOLD)
            .then(|| Arc::from(Backtrace::force_capture().to_string()));
        let mut pending = PENDING.lock().unwrap_or_else(PoisonError::into_inner);
        if pending.spans.len() >= PENDING_SPANS {
            pending.dropped = pending.dropped.saturating_add(1);
            return;
        }
        pending.spans.push(SpanRecord {
            name,
            cascade: state.cascade,
            thread: state.thread,
            depth: state.depth,
            start,
            end,
            stack,
        });
    }
}

#[must_use]
pub struct Cascade {
    previous: Option<Option<CascadeId>>,
    _span: Span,
}

impl Cascade {
    pub fn begin(name: &'static str) -> Self {
        Self::enter(
            name,
            Some(CascadeId(NEXT_CASCADE.fetch_add(1, Ordering::Relaxed))),
        )
    }

    pub fn enter(name: &'static str, id: Option<CascadeId>) -> Self {
        if id.is_none() || !gpui::trace_enabled() {
            return Cascade {
                previous: None,
                _span: Span(None),
            };
        }
        let previous = THREAD.with(|thread| {
            let state = thread.get();
            thread.set(ThreadState {
                cascade: id,
                ..state
            });
            state.cascade
        });
        Cascade {
            previous: Some(previous),
            _span: Span::open(name),
        }
    }

    pub fn current() -> Option<CascadeId> {
        THREAD.with(|thread| thread.get().cascade)
    }
}

impl Drop for Cascade {
    fn drop(&mut self) {
        if let Some(previous) = self.previous {
            THREAD.with(|thread| {
                thread.set(ThreadState {
                    cascade: previous,
                    ..thread.get()
                })
            });
        }
    }
}

pub(crate) fn drain() -> (Vec<SpanRecord>, u64) {
    let mut pending = PENDING.lock().unwrap_or_else(PoisonError::into_inner);
    let dropped = mem::take(&mut pending.dropped);
    (mem::take(&mut pending.spans), dropped)
}
