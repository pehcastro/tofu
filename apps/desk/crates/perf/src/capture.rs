use std::fmt;
use std::time::Instant;

pub(crate) struct Shot {
    pub at: Instant,
    pub width: usize,
    pub height: usize,
    pub bgra: Vec<u8>,
}

#[derive(Default)]
pub(crate) struct Shots {
    pub taken: Vec<Shot>,
    pub bytes: usize,
    pub dropped: usize,
}

#[derive(Debug)]
pub(crate) enum CaptureError {
    Handle(raw_window_handle::HandleError),
    NotWin32,
    #[cfg(not(windows))]
    Unsupported,
    Start(String),
    Stop(String),
}

impl fmt::Display for CaptureError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Handle(error) => write!(f, "the window gave no handle: {error}"),
            Self::NotWin32 => write!(f, "the window handle is not a Win32 HWND"),
            #[cfg(not(windows))]
            Self::Unsupported => write!(f, "frame capture runs on Windows only"),
            Self::Start(error) => write!(f, "Windows.Graphics.Capture did not start: {error}"),
            Self::Stop(error) => write!(f, "Windows.Graphics.Capture did not stop: {error}"),
        }
    }
}

#[cfg(windows)]
pub(crate) use self::wgc::{Capture, refresh_hz};

#[cfg(windows)]
mod wgc {
    use std::mem;
    use std::sync::{Arc, Mutex, PoisonError};
    use std::time::Instant;

    use gpui::Window;
    use raw_window_handle::{HasWindowHandle, RawWindowHandle};
    use windows_capture::capture::{CaptureControl, Context, GraphicsCaptureApiHandler};
    use windows_capture::frame::{Error as FrameError, Frame};
    use windows_capture::graphics_capture_api::InternalCaptureControl;
    use windows_capture::settings::{
        ColorFormat, CursorCaptureSettings, DirtyRegionSettings, DrawBorderSettings,
        MinimumUpdateIntervalSettings, SecondaryWindowSettings, Settings,
    };
    use windows_capture::window::Window as CaptureWindow;

    use super::{CaptureError, Shot, Shots};
    use crate::limits::RECORD_MAX_BYTES;

    pub(crate) struct Grabber {
        shots: Arc<Mutex<Shots>>,
        client: (u32, u32),
    }

    impl GraphicsCaptureApiHandler for Grabber {
        type Flags = Grabber;
        type Error = FrameError;

        fn new(context: Context<Self::Flags>) -> Result<Self, Self::Error> {
            Ok(context.flags)
        }

        fn on_frame_arrived(
            &mut self,
            frame: &mut Frame,
            _: InternalCaptureControl,
        ) -> Result<(), Self::Error> {
            let at = Instant::now();
            let (width, height) = (frame.width(), frame.height());
            let client_width = self.client.0.min(width);
            let client_height = self.client.1.min(height);
            let left = (width - client_width) / 2;
            let top = height.saturating_sub(client_height.saturating_add(left));
            let mut buffer = frame.buffer_crop(
                left,
                top,
                left.saturating_add(client_width),
                top.saturating_add(client_height),
            )?;
            let (width, height) = (buffer.width() as usize, buffer.height() as usize);
            let bgra = buffer.as_nopadding_buffer()?.to_vec();
            let mut shots = self.shots.lock().unwrap_or_else(PoisonError::into_inner);
            let bytes = shots.bytes.saturating_add(bgra.len());
            if bytes > RECORD_MAX_BYTES {
                shots.dropped = shots.dropped.saturating_add(1);
                return Ok(());
            }
            shots.bytes = bytes;
            shots.taken.push(Shot {
                at,
                width,
                height,
                bgra,
            });
            Ok(())
        }
    }

    fn target(window: &Window) -> Result<CaptureWindow, CaptureError> {
        let handle = HasWindowHandle::window_handle(window).map_err(CaptureError::Handle)?;
        let RawWindowHandle::Win32(win32) = handle.as_raw() else {
            return Err(CaptureError::NotWin32);
        };
        Ok(CaptureWindow::from_raw_hwnd(
            std::ptr::without_provenance_mut(win32.hwnd.get().cast_unsigned()),
        ))
    }

    pub(crate) fn refresh_hz(window: &Window) -> Option<u32> {
        target(window).ok()?.monitor()?.refresh_rate().ok()
    }

    pub(crate) struct Capture {
        control: CaptureControl<Grabber, FrameError>,
        shots: Arc<Mutex<Shots>>,
    }

    impl Capture {
        pub fn start(window: &Window) -> Result<Self, CaptureError> {
            let target = target(window)?;
            let size = window.viewport_size();
            let scale = window.scale_factor();
            let shots = Arc::new(Mutex::new(Shots::default()));
            let settings = Settings::new(
                target,
                CursorCaptureSettings::WithoutCursor,
                DrawBorderSettings::Default,
                SecondaryWindowSettings::Default,
                MinimumUpdateIntervalSettings::Default,
                DirtyRegionSettings::Default,
                ColorFormat::Bgra8,
                Grabber {
                    shots: Arc::clone(&shots),
                    client: (
                        (size.width.as_f32() * scale).round() as u32,
                        (size.height.as_f32() * scale).round() as u32,
                    ),
                },
            );
            let control = Grabber::start_free_threaded(settings)
                .map_err(|error| CaptureError::Start(error.to_string()))?;
            Ok(Capture { control, shots })
        }

        pub fn stop(self) -> (Shots, Option<CaptureError>) {
            let stopped = self.control.stop();
            let shots = mem::take(&mut *self.shots.lock().unwrap_or_else(PoisonError::into_inner));
            (
                shots,
                stopped
                    .err()
                    .map(|error| CaptureError::Stop(error.to_string())),
            )
        }
    }
}

#[cfg(not(windows))]
pub(crate) struct Capture;

#[cfg(not(windows))]
pub(crate) fn refresh_hz(_: &gpui::Window) -> Option<u32> {
    None
}

#[cfg(not(windows))]
impl Capture {
    pub fn start(_: &gpui::Window) -> Result<Self, CaptureError> {
        Err(CaptureError::Unsupported)
    }

    pub fn stop(self) -> (Shots, Option<CaptureError>) {
        (Shots::default(), None)
    }
}
