use std::collections::VecDeque;
use std::io::{self, Stdout, Write};
use std::ops::{Deref, DerefMut};
use std::process::Command;
use std::sync::mpsc::{self, Receiver};
use std::time::{Duration, Instant};

use anstyle::{AnsiColor, Color as AnsiStyleColor, Style as AnsiStyle};
use base64::Engine;
use crossterm::event::{self, Event as TerminalEvent, KeyCode, KeyEvent, KeyModifiers};
use crossterm::execute;
use crossterm::terminal::{
    EnterAlternateScreen, LeaveAlternateScreen, disable_raw_mode, enable_raw_mode,
};
use ratatui::Terminal;
use ratatui::backend::CrosstermBackend;
use ratatui::layout::{Constraint, Layout, Rect};
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, Borders, Cell, Paragraph, Row, Table, TableState, Wrap};
use url::Url;

use crate::discover::LocalService;
use crate::protocol::{ClientFrame, EdgeFrame, Header};

const MAX_EXCHANGES: usize = 500;
const MAX_CAPTURE_BYTES: usize = 64 * 1024;
const BODY_PREVIEW_LINES: usize = 12;
const NOTICE_LIFETIME: Duration = Duration::from_secs(3);
const HEARTBEAT: Duration = Duration::from_secs(1);

const PROMPT_SUCCESS: AnsiStyle =
    AnsiStyle::new().fg_color(Some(AnsiStyleColor::Ansi(AnsiColor::Green)));
const PROMPT_ACCENT: AnsiStyle = AnsiStyle::new()
    .fg_color(Some(AnsiStyleColor::Ansi(AnsiColor::Green)))
    .bold();
const PROMPT_MUTED: AnsiStyle =
    AnsiStyle::new().fg_color(Some(AnsiStyleColor::Ansi(AnsiColor::BrightBlack)));
const PROMPT_FADED: AnsiStyle = AnsiStyle::new()
    .fg_color(Some(AnsiStyleColor::Ansi(AnsiColor::BrightBlack)))
    .dimmed();
const PROMPT_ORIGIN: AnsiStyle =
    AnsiStyle::new().fg_color(Some(AnsiStyleColor::Ansi(AnsiColor::Cyan)));
const PROMPT_BOLD: AnsiStyle = AnsiStyle::new().bold();

const ACCENT: Color = Color::Rgb(126, 231, 135);
const LINK: Color = Color::Rgb(121, 192, 255);
const MUTED: Color = Color::Rgb(125, 133, 144);
const SELECTION: Color = Color::Rgb(32, 39, 48);
const CANVAS: Color = Color::Rgb(13, 17, 23);

struct Screen(Terminal<CrosstermBackend<Stdout>>);

impl Screen {
    fn enter() -> io::Result<Self> {
        enable_raw_mode()?;
        let mut stdout = io::stdout();
        if let Err(error) = execute!(stdout, EnterAlternateScreen) {
            let _ = disable_raw_mode();
            return Err(error);
        }
        match Terminal::new(CrosstermBackend::new(stdout)) {
            Ok(terminal) => Ok(Self(terminal)),
            Err(error) => {
                let _ = disable_raw_mode();
                let _ = execute!(io::stdout(), LeaveAlternateScreen);
                Err(error)
            }
        }
    }

    fn copy(&mut self, value: &str) -> io::Result<()> {
        let encoded = base64::engine::general_purpose::STANDARD.encode(value);
        write!(self.0.backend_mut(), "\x1b]52;c;{encoded}\x07")?;
        self.0.backend_mut().flush()
    }
}

impl Deref for Screen {
    type Target = Terminal<CrosstermBackend<Stdout>>;

    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl DerefMut for Screen {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.0
    }
}

impl Drop for Screen {
    fn drop(&mut self) {
        let _ = disable_raw_mode();
        let _ = execute!(self.0.backend_mut(), LeaveAlternateScreen);
        let _ = self.0.show_cursor();
    }
}

pub fn choose_service(services: &[LocalService]) -> io::Result<Option<usize>> {
    if services.is_empty() {
        return Ok(None);
    }
    let mut stderr = io::stderr();
    let rows = service_rows(services);
    let mut view = View::new(rows.len());

    write!(stderr, "{}", prompt_header(rows.len()))?;
    for line in view.lines(&rows) {
        writeln!(stderr, "{line}")?;
    }
    write!(stderr, "{}", view.hint())?;
    stderr.flush()?;

    enable_raw_mode()?;
    let choice = loop {
        let TerminalEvent::Key(key) = event::read()? else {
            continue;
        };
        let before = (view.selected, view.offset);
        match key.code {
            KeyCode::Up | KeyCode::Char('k') => view.select(view.selected.saturating_sub(1)),
            KeyCode::Down | KeyCode::Char('j') => view.select(view.selected + 1),
            KeyCode::Home | KeyCode::Char('g') => view.select(0),
            KeyCode::End | KeyCode::Char('G') => view.select(rows.len() - 1),
            KeyCode::Char(digit @ '1'..='9') => {
                let index = digit as usize - '1' as usize;
                if index < rows.len() {
                    view.select(index);
                    break Some(view.selected);
                }
            }
            KeyCode::Enter => break Some(view.selected),
            KeyCode::Esc | KeyCode::Char('q') => break None,
            KeyCode::Char('c') if key.modifiers.contains(KeyModifiers::CONTROL) => break None,
            _ => {}
        }
        if (view.selected, view.offset) != before {
            redraw_rows(&mut stderr, &rows, &view)?;
        }
    };
    let _ = disable_raw_mode();

    finish_prompt(&mut stderr, &rows, &view, choice)?;
    Ok(choice)
}

pub fn choose_edge(edges: &[String]) -> io::Result<Option<usize>> {
    if edges.is_empty() {
        return Ok(None);
    }
    let mut stderr = io::stderr();
    let mut view = View::new(edges.len());

    writeln!(stderr, "Choose an edge")?;
    for line in edge_lines(edges, &view) {
        writeln!(stderr, "{line}")?;
    }
    write!(stderr, "{}", edge_hint(edges.len()))?;
    stderr.flush()?;

    enable_raw_mode()?;
    let choice = loop {
        let TerminalEvent::Key(key) = event::read()? else {
            continue;
        };
        let before = (view.selected, view.offset);
        match key.code {
            KeyCode::Up | KeyCode::Char('k') => view.select(view.selected.saturating_sub(1)),
            KeyCode::Down | KeyCode::Char('j') => view.select(view.selected + 1),
            KeyCode::Home | KeyCode::Char('g') => view.select(0),
            KeyCode::End | KeyCode::Char('G') => view.select(edges.len() - 1),
            KeyCode::Char(digit @ '1'..='9') => {
                let index = digit as usize - '1' as usize;
                if index < edges.len() {
                    view.select(index);
                    break Some(view.selected);
                }
            }
            KeyCode::Enter => break Some(view.selected),
            KeyCode::Esc | KeyCode::Char('q') => break None,
            KeyCode::Char('c') if key.modifiers.contains(KeyModifiers::CONTROL) => break None,
            _ => {}
        }
        if (view.selected, view.offset) != before {
            redraw_edges(&mut stderr, edges, &view)?;
        }
    };
    let _ = disable_raw_mode();
    finish_edge_prompt(&mut stderr, edges, &view, choice)?;
    Ok(choice)
}

fn edge_lines(edges: &[String], view: &View) -> Vec<String> {
    let width = edges.len().to_string().len();
    (view.offset..view.offset + view.height)
        .map(|index| {
            let number = format!("{:>width$}", index + 1, width = width);
            let fade = (index == view.offset && view.hidden_above() > 0)
                || (index + 1 == view.offset + view.height && view.hidden_below() > 0);
            if fade {
                format!("  {PROMPT_FADED}{number} {}{PROMPT_FADED:#}", edges[index])
            } else if index == view.selected {
                format!(
                    "{PROMPT_SUCCESS}›{PROMPT_SUCCESS:#} {PROMPT_MUTED}{number}{PROMPT_MUTED:#} {PROMPT_ACCENT}{}{PROMPT_ACCENT:#}",
                    edges[index]
                )
            } else {
                format!("  {PROMPT_MUTED}{number}{PROMPT_MUTED:#} {}", edges[index])
            }
        })
        .collect()
}

fn edge_hint(count: usize) -> String {
    format!(
        "  ↑↓ select · 1-{} jump · enter connect · esc cancel",
        count.min(9)
    )
}

fn redraw_edges(writer: &mut impl Write, edges: &[String], view: &View) -> io::Result<()> {
    write!(writer, "\r\x1b[{}A", view.height)?;
    for line in edge_lines(edges, view) {
        write!(writer, "\x1b[2K{line}\r\n")?;
    }
    write!(writer, "\x1b[2K{}", edge_hint(edges.len()))?;
    writer.flush()
}

fn finish_edge_prompt(
    writer: &mut impl Write,
    edges: &[String],
    view: &View,
    choice: Option<usize>,
) -> io::Result<()> {
    write!(writer, "\r\x1b[{}A", view.height)?;
    for _ in 0..view.height {
        writeln!(writer, "\x1b[2K")?;
    }
    write!(writer, "\x1b[2K\r\x1b[{}A", view.height)?;
    match choice {
        Some(selected) => writeln!(
            writer,
            "{PROMPT_SUCCESS}✓{PROMPT_SUCCESS:#} Edge  {PROMPT_ORIGIN}{}{PROMPT_ORIGIN:#}",
            edges[selected]
        )?,
        None => writeln!(writer, "Cancelled")?,
    }
    writer.flush()
}

const MAX_VISIBLE: usize = 7;

struct View {
    selected: usize,
    offset: usize,
    height: usize,
    total: usize,
}

impl View {
    fn new(total: usize) -> Self {
        Self {
            selected: 0,
            offset: 0,
            height: total.min(MAX_VISIBLE),
            total,
        }
    }

    fn scrolls(&self) -> bool {
        self.total > self.height
    }

    fn select(&mut self, index: usize) {
        self.selected = index.min(self.total - 1);
        let margin = usize::from(self.scrolls());
        if self.selected < self.offset + margin {
            self.offset = self.selected.saturating_sub(margin);
        }
        if self.selected + margin + 1 > self.offset + self.height {
            self.offset = self.selected + margin + 2 - self.height;
        }
        self.offset = self.offset.min(self.total - self.height);
    }

    fn hidden_above(&self) -> usize {
        self.offset
    }

    fn hidden_below(&self) -> usize {
        self.total - self.offset - self.height
    }

    fn lines(&self, rows: &[ServiceRow]) -> Vec<String> {
        let width = self.total.to_string().len();
        (self.offset..self.offset + self.height)
            .map(|index| {
                let fade = (index == self.offset && self.hidden_above() > 0)
                    || (index + 1 == self.offset + self.height && self.hidden_below() > 0);
                prompt_row(&rows[index], index, width, index == self.selected, fade)
            })
            .collect()
    }

    fn hint(&self) -> String {
        let jump = self.total.min(9);
        let mut hint = format!("  ↑↓ select · 1-{jump} jump · enter publish · esc cancel");
        if self.scrolls() {
            hint.push_str(&format!(
                "{PROMPT_MUTED} · {} of {}{PROMPT_MUTED:#}",
                self.selected + 1,
                self.total
            ));
        }
        hint
    }
}

pub enum LoginSetup {
    Anonymous,
    Secret(String),
    Cancelled,
}

pub fn choose_login_setup(generated: &str) -> io::Result<LoginSetup> {
    const CHOICES: [(&str, &str); 2] = [
        ("Anyone may publish", "no accounts, no reserved names"),
        (
            "Require an account",
            "accounts, reserved names, higher quotas",
        ),
    ];

    let mut stderr = io::stderr();
    writeln!(stderr, "Who may use this edge?")?;
    for (index, (title, detail)) in CHOICES.iter().enumerate() {
        writeln!(stderr, "{}", choice_row(index, title, detail, index == 0))?;
    }
    write!(
        stderr,
        "  ↑↓ select · 1-2 jump · enter confirm · esc cancel"
    )?;
    stderr.flush()?;

    enable_raw_mode()?;
    let mut selected = 0;
    let picked = loop {
        let TerminalEvent::Key(key) = event::read()? else {
            continue;
        };
        let before = selected;
        match key.code {
            KeyCode::Up | KeyCode::Char('k') => selected = 0,
            KeyCode::Down | KeyCode::Char('j') => selected = 1,
            KeyCode::Char('1') => break Some(0),
            KeyCode::Char('2') => break Some(1),
            KeyCode::Enter => break Some(selected),
            KeyCode::Esc | KeyCode::Char('q') => break None,
            KeyCode::Char('c') if key.modifiers.contains(KeyModifiers::CONTROL) => break None,
            _ => {}
        }
        if selected != before {
            write!(stderr, "\r\x1b[{}A", CHOICES.len())?;
            for (index, (title, detail)) in CHOICES.iter().enumerate() {
                write!(
                    stderr,
                    "\x1b[2K{}\r\n",
                    choice_row(index, title, detail, index == selected)
                )?;
            }
            write!(
                stderr,
                "\x1b[2K  ↑↓ select · 1-2 jump · enter confirm · esc cancel"
            )?;
            stderr.flush()?;
        }
    };
    let _ = disable_raw_mode();

    let collapse = |writer: &mut io::Stderr| -> io::Result<()> {
        write!(writer, "\r\x1b[{}A", CHOICES.len())?;
        for _ in 0..CHOICES.len() {
            writeln!(writer, "\x1b[2K")?;
        }
        write!(writer, "\x1b[2K\r\x1b[{}A", CHOICES.len())
    };

    match picked {
        None => {
            collapse(&mut stderr)?;
            writeln!(stderr, "Cancelled")?;
            Ok(LoginSetup::Cancelled)
        }
        Some(0) => {
            collapse(&mut stderr)?;
            writeln!(
                stderr,
                "{PROMPT_SUCCESS}✓{PROMPT_SUCCESS:#} Anyone may publish  {PROMPT_MUTED}no accounts{PROMPT_MUTED:#}"
            )?;
            Ok(LoginSetup::Anonymous)
        }
        Some(_) => {
            collapse(&mut stderr)?;
            writeln!(
                stderr,
                "{PROMPT_SUCCESS}✓{PROMPT_SUCCESS:#} Require an account"
            )?;
            let secret = read_secret(&mut stderr, generated)?;
            match secret {
                Some(secret) => Ok(LoginSetup::Secret(secret)),
                None => {
                    writeln!(stderr, "Cancelled")?;
                    Ok(LoginSetup::Cancelled)
                }
            }
        }
    }
}

fn choice_row(index: usize, title: &str, detail: &str, selected: bool) -> String {
    let number = index + 1;
    if selected {
        format!(
            "{PROMPT_SUCCESS}›{PROMPT_SUCCESS:#} {PROMPT_MUTED}{number}{PROMPT_MUTED:#} {PROMPT_ACCENT}{title:<20}{PROMPT_ACCENT:#}  {PROMPT_MUTED}{detail}{PROMPT_MUTED:#}"
        )
    } else {
        format!(
            "  {PROMPT_MUTED}{number}{PROMPT_MUTED:#} {title:<20}  {PROMPT_MUTED}{detail}{PROMPT_MUTED:#}"
        )
    }
}

fn read_secret(writer: &mut io::Stderr, generated: &str) -> io::Result<Option<String>> {
    write!(
        writer,
        "  Approval secret {PROMPT_MUTED}(enter to generate one){PROMPT_MUTED:#}\n  › "
    )?;
    writer.flush()?;

    enable_raw_mode()?;
    let mut typed = String::new();
    let confirmed = loop {
        let TerminalEvent::Key(key) = event::read()? else {
            continue;
        };
        match key.code {
            KeyCode::Enter => break true,
            KeyCode::Esc => break false,
            KeyCode::Char('c') if key.modifiers.contains(KeyModifiers::CONTROL) => break false,
            KeyCode::Backspace => {
                if typed.pop().is_some() {
                    write!(writer, "\u{8} \u{8}")?;
                    writer.flush()?;
                }
            }
            KeyCode::Char(character) if !character.is_control() => {
                typed.push(character);
                write!(writer, "•")?;
                writer.flush()?;
            }
            _ => {}
        }
    };
    let _ = disable_raw_mode();
    writeln!(writer)?;

    if !confirmed {
        return Ok(None);
    }
    let secret = if typed.trim().is_empty() {
        writeln!(
            writer,
            "  {PROMPT_MUTED}generated{PROMPT_MUTED:#}  {PROMPT_BOLD}{generated}{PROMPT_BOLD:#}\n  {PROMPT_MUTED}Save it now; \
             this edge will not show it again.{PROMPT_MUTED:#}"
        )?;
        generated.to_string()
    } else {
        typed
    };
    Ok(Some(secret))
}

struct ServiceRow {
    kind: String,
    origin: String,
    detail: String,
}

fn service_rows(services: &[LocalService]) -> Vec<ServiceRow> {
    let width = services
        .iter()
        .map(|service| service.kind.chars().count())
        .max()
        .unwrap_or(0);
    services
        .iter()
        .map(|service| ServiceRow {
            kind: format!("{:<width$}", service.kind, width = width),
            origin: origin(&service.url),
            detail: match (&service.detail, service.status) {
                (Some(detail), 200..=299) => detail.clone(),
                (Some(detail), status) => format!("{detail} · HTTP {status}"),
                (None, 200..=299) => String::new(),
                (None, status) => format!("HTTP {status}"),
            },
        })
        .collect()
}

fn origin(url: &Url) -> String {
    match url.port() {
        Some(port) => format!(":{port}"),
        None => url.as_str().trim_end_matches('/').to_string(),
    }
}

fn prompt_header(count: usize) -> String {
    let plural = if count == 1 { "service" } else { "services" };
    format!("Found {count} local {plural}\n")
}

fn prompt_row(row: &ServiceRow, index: usize, width: usize, selected: bool, fade: bool) -> String {
    let number = format!("{:>width$}", index + 1, width = width);
    if fade {
        let mut line = format!("  {PROMPT_FADED}{number} {}  {:>6}", row.kind, row.origin);
        if !row.detail.is_empty() {
            line.push_str(&format!("  {}", row.detail));
        }
        line.push_str(&format!("{PROMPT_FADED:#}"));
        return line;
    }

    let mut line = if selected {
        format!(
            "{PROMPT_SUCCESS}›{PROMPT_SUCCESS:#} {PROMPT_MUTED}{number}{PROMPT_MUTED:#} {PROMPT_ACCENT}{}{PROMPT_ACCENT:#}",
            row.kind
        )
    } else {
        format!("  {PROMPT_MUTED}{number}{PROMPT_MUTED:#} {}", row.kind)
    };
    line.push_str(&format!(
        "  {PROMPT_ORIGIN}{:>6}{PROMPT_ORIGIN:#}",
        row.origin
    ));
    if !row.detail.is_empty() {
        line.push_str(&format!("  {PROMPT_MUTED}{}{PROMPT_MUTED:#}", row.detail));
    }
    line
}

fn redraw_rows(writer: &mut impl Write, rows: &[ServiceRow], view: &View) -> io::Result<()> {
    write!(writer, "\r\x1b[{}A", view.height)?;
    for line in view.lines(rows) {
        write!(writer, "\x1b[2K{line}\r\n")?;
    }
    write!(writer, "\x1b[2K{}", view.hint())?;
    writer.flush()
}

fn finish_prompt(
    writer: &mut impl Write,
    rows: &[ServiceRow],
    view: &View,
    choice: Option<usize>,
) -> io::Result<()> {
    write!(writer, "\r\x1b[{}A", view.height)?;
    for _ in 0..view.height {
        writeln!(writer, "\x1b[2K")?;
    }
    write!(writer, "\x1b[2K\r\x1b[{}A", view.height)?;
    match choice {
        Some(selected) => {
            let row = &rows[selected];
            writeln!(
                writer,
                "{PROMPT_SUCCESS}✓{PROMPT_SUCCESS:#} {}  {PROMPT_ORIGIN}{}{PROMPT_ORIGIN:#}",
                row.kind.trim_end(),
                row.origin
            )?;
        }
        None => writeln!(writer, "Cancelled")?,
    }
    writer.flush()
}

pub struct LiveUi {
    screen: Screen,
    keys: Receiver<KeyEvent>,
    dashboard: Dashboard,
    dirty: bool,
    drawn: Instant,
}

pub enum Action {
    Quit,
    Replay(Replay),
}

pub struct Replay {
    pub id: u64,
    pub method: String,
    pub path: String,
    pub headers: Vec<Header>,
    pub body: Vec<u8>,
}

enum Intent {
    Quit,
    Replay,
    CopyUrl,
    OpenUrl,
    CopyCurl,
    None,
}

struct Dashboard {
    public_url: String,
    target: String,
    exchanges: VecDeque<Exchange>,
    selected: usize,
    following: bool,
    pane: Pane,
    filter: String,
    filter_editing: bool,
    notice: Option<Notice>,
    next_local_id: u64,
    online: bool,
}

#[derive(Clone, Copy, PartialEq)]
enum Pane {
    Request,
    Response,
}

struct Notice {
    text: String,
    shown: Instant,
    sticky: bool,
}

struct Exchange {
    id: u64,
    method: String,
    path: String,
    request_headers: Vec<Header>,
    request_body: Vec<u8>,
    response_headers: Vec<Header>,
    response_body: Vec<u8>,
    status: Option<u16>,
    error: Option<String>,
    started: Instant,
    duration_ms: Option<u128>,
    replayed: bool,
}

impl LiveUi {
    pub fn new(public_url: String, target: String) -> io::Result<Self> {
        let screen = Screen::enter()?;
        let (keys, receiver) = mpsc::channel();
        std::thread::spawn(move || {
            while let Ok(event) = event::read() {
                if let TerminalEvent::Key(key) = event
                    && keys.send(key).is_err()
                {
                    break;
                }
            }
        });

        Ok(Self {
            screen,
            keys: receiver,
            dashboard: Dashboard::new(public_url, target),
            dirty: true,
            drawn: Instant::now(),
        })
    }

    pub fn apply_edge(&mut self, frame: &EdgeFrame) {
        self.dashboard.apply_edge(frame);
        self.dirty = true;
    }

    pub fn apply_client(&mut self, frame: &ClientFrame) {
        self.dashboard.apply_client(frame);
        self.dirty = true;
    }

    pub fn set_online(&mut self, online: bool) {
        self.dashboard.online = online;
        self.dashboard.notice =
            (!online).then(|| Notice::sticky("edge disconnected, reconnecting"));
        self.dirty = true;
    }

    pub fn update(&mut self) -> Option<Action> {
        while let Ok(key) = self.keys.try_recv() {
            self.dirty = true;
            match self.dashboard.key(key) {
                Intent::Quit => return Some(Action::Quit),
                Intent::Replay => {
                    if let Some(replay) = self.dashboard.begin_replay() {
                        return Some(Action::Replay(replay));
                    }
                }
                Intent::CopyUrl => {
                    let url = self.dashboard.public_url.clone();
                    self.dashboard.notice = Some(match self.screen.copy(&url) {
                        Ok(()) => Notice::new("public URL copied"),
                        Err(_) => Notice::new("could not reach the clipboard"),
                    });
                }
                Intent::OpenUrl => {
                    let url = self.dashboard.public_url.clone();
                    self.dashboard.notice = Some(match open_url(&url) {
                        Ok(()) => Notice::new("opened public URL"),
                        Err(_) => Notice::new("no browser command available"),
                    });
                }
                Intent::CopyCurl => {
                    if let Some(command) = self.dashboard.export_curl() {
                        self.dashboard.notice = Some(match self.screen.copy(&command) {
                            Ok(()) => Notice::new("curl command copied"),
                            Err(_) => Notice::new("could not reach the clipboard"),
                        });
                    }
                }
                Intent::None => {}
            }
        }
        None
    }

    pub fn draw(&mut self) -> io::Result<()> {
        if self.dashboard.expire_notice() || self.drawn.elapsed() >= HEARTBEAT {
            self.dirty = true;
        }
        if !self.dirty {
            return Ok(());
        }
        let dashboard = &self.dashboard;
        self.screen.draw(|frame| draw(frame, dashboard))?;
        self.dirty = false;
        self.drawn = Instant::now();
        Ok(())
    }
}

impl Notice {
    fn new(text: impl Into<String>) -> Self {
        Self {
            text: text.into(),
            shown: Instant::now(),
            sticky: false,
        }
    }

    fn sticky(text: impl Into<String>) -> Self {
        Self {
            sticky: true,
            ..Self::new(text)
        }
    }
}

impl Dashboard {
    fn new(public_url: String, target: String) -> Self {
        Self {
            public_url,
            target,
            exchanges: VecDeque::new(),
            selected: 0,
            following: true,
            pane: Pane::Response,
            filter: String::new(),
            filter_editing: false,
            notice: None,
            next_local_id: u64::MAX,
            online: true,
        }
    }

    fn apply_edge(&mut self, frame: &EdgeFrame) {
        match frame {
            EdgeFrame::RequestStart {
                id,
                method,
                path,
                headers,
            } => {
                self.exchanges.push_front(Exchange::new(
                    *id,
                    method.clone(),
                    path.clone(),
                    headers.clone(),
                    Vec::new(),
                    false,
                ));
                self.exchanges.truncate(MAX_EXCHANGES);
                if self.following {
                    self.select(0);
                } else {
                    self.selected = (self.selected + 1).min(MAX_EXCHANGES - 1);
                }
            }
            EdgeFrame::RequestChunk { id, body } => {
                if let Some(exchange) = self.exchange_mut(*id) {
                    capture(&mut exchange.request_body, body);
                }
            }
            EdgeFrame::RequestEnd { .. } => {}
            EdgeFrame::Cancel { id } => {
                if let Some(exchange) = self.exchange_mut(*id) {
                    exchange.finish(Some("cancelled".into()));
                }
            }
        }
    }

    fn apply_client(&mut self, frame: &ClientFrame) {
        let id = match frame {
            ClientFrame::Start { id, .. }
            | ClientFrame::Chunk { id, .. }
            | ClientFrame::End { id }
            | ClientFrame::Error { id, .. } => *id,
        };
        let Some(exchange) = self.exchange_mut(id) else {
            return;
        };
        match frame {
            ClientFrame::Start {
                status, headers, ..
            } => {
                exchange.status = Some(*status);
                exchange.response_headers.clone_from(headers);
            }
            ClientFrame::Chunk { body, .. } => capture(&mut exchange.response_body, body),
            ClientFrame::End { .. } => exchange.finish(None),
            ClientFrame::Error { reason, .. } => exchange.finish(Some(reason.clone())),
        }
    }

    fn exchange_mut(&mut self, id: u64) -> Option<&mut Exchange> {
        self.exchanges.iter_mut().find(|exchange| exchange.id == id)
    }

    fn expire_notice(&mut self) -> bool {
        let expired = self
            .notice
            .as_ref()
            .is_some_and(|notice| !notice.sticky && notice.shown.elapsed() >= NOTICE_LIFETIME);
        if expired {
            self.notice = None;
        }
        expired
    }

    fn key(&mut self, key: KeyEvent) -> Intent {
        if key.modifiers.contains(KeyModifiers::CONTROL) {
            return if key.code == KeyCode::Char('c') {
                Intent::Quit
            } else {
                Intent::None
            };
        }
        if self.filter_editing {
            match key.code {
                KeyCode::Esc => {
                    self.filter.clear();
                    self.filter_editing = false;
                }
                KeyCode::Enter => self.filter_editing = false,
                KeyCode::Backspace => {
                    self.filter.pop();
                }
                KeyCode::Char(character) => self.filter.push(character),
                _ => {}
            }
            self.select(0);
            return Intent::None;
        }
        match key.code {
            KeyCode::Char('q') | KeyCode::Esc => return Intent::Quit,
            KeyCode::Char('r') => return Intent::Replay,
            KeyCode::Char('c') => return Intent::CopyUrl,
            KeyCode::Char('o') => return Intent::OpenUrl,
            KeyCode::Char('e') => return Intent::CopyCurl,
            KeyCode::Up | KeyCode::Char('k') => self.select(self.selected.saturating_sub(1)),
            KeyCode::Down | KeyCode::Char('j') => self.select(self.selected + 1),
            KeyCode::Home | KeyCode::Char('g') => self.select(0),
            KeyCode::Tab | KeyCode::Left | KeyCode::Right => {
                self.pane = match self.pane {
                    Pane::Request => Pane::Response,
                    Pane::Response => Pane::Request,
                };
            }
            KeyCode::Char(' ') => {
                self.following = !self.following;
                if self.following {
                    self.select(0);
                }
            }
            KeyCode::Char('/') => self.filter_editing = true,
            _ => {}
        }
        Intent::None
    }

    fn select(&mut self, index: usize) {
        let last = self.visible_exchanges().len().saturating_sub(1);
        self.selected = index.min(last);
        self.following = self.selected == 0;
    }

    fn visible_exchanges(&self) -> Vec<&Exchange> {
        let filter = self.filter.to_ascii_lowercase();
        self.exchanges
            .iter()
            .filter(|exchange| exchange.matches(&filter))
            .collect()
    }

    fn selection(&self) -> Option<&Exchange> {
        let visible = self.visible_exchanges();
        visible
            .get(self.selected.min(visible.len().saturating_sub(1)))
            .copied()
    }

    fn begin_replay(&mut self) -> Option<Replay> {
        let source = self.selection()?;
        let replay = Replay {
            id: self.next_local_id,
            method: source.method.clone(),
            path: source.path.clone(),
            headers: source.request_headers.clone(),
            body: source.request_body.clone(),
        };
        self.next_local_id = self.next_local_id.saturating_sub(1);
        self.exchanges.push_front(Exchange::new(
            replay.id,
            replay.method.clone(),
            replay.path.clone(),
            replay.headers.clone(),
            replay.body.clone(),
            true,
        ));
        self.exchanges.truncate(MAX_EXCHANGES);
        self.following = true;
        self.select(0);
        self.notice = Some(Notice::new("replayed against the local service"));
        Some(replay)
    }

    fn export_curl(&self) -> Option<String> {
        let exchange = self.selection()?;
        let url = format!(
            "{}{}",
            self.public_url.trim_end_matches('/'),
            exchange.path.as_str()
        );
        let mut command = format!("curl --request {} {}", exchange.method, shell_quote(&url));
        for (name, value) in &exchange.request_headers {
            if sensitive_header(name) || generated_header(name) {
                continue;
            }
            let header = format!("{name}: {}", String::from_utf8_lossy(value));
            command.push_str(" --header ");
            command.push_str(&shell_quote(&header));
        }
        if let Some(body) = body_text(&exchange.request_headers, &exchange.request_body)
            && !body.is_empty()
        {
            command.push_str(" --data-raw ");
            command.push_str(&shell_quote(&body));
        }
        Some(command)
    }
}

impl Exchange {
    fn new(
        id: u64,
        method: String,
        path: String,
        request_headers: Vec<Header>,
        request_body: Vec<u8>,
        replayed: bool,
    ) -> Self {
        Self {
            id,
            method,
            path,
            request_headers,
            request_body,
            response_headers: Vec::new(),
            response_body: Vec::new(),
            status: None,
            error: None,
            started: Instant::now(),
            duration_ms: None,
            replayed,
        }
    }

    fn finish(&mut self, error: Option<String>) {
        self.error = error.or_else(|| self.error.take());
        self.duration_ms = Some(self.started.elapsed().as_millis());
    }

    fn matches(&self, filter: &str) -> bool {
        filter.is_empty()
            || self.method.to_ascii_lowercase().contains(filter)
            || self.path.to_ascii_lowercase().contains(filter)
            || self
                .status
                .is_some_and(|status| status.to_string().contains(filter))
    }

    fn pane(&self, pane: Pane) -> (&[Header], &[u8]) {
        match pane {
            Pane::Request => (&self.request_headers, &self.request_body),
            Pane::Response => (&self.response_headers, &self.response_body),
        }
    }

    fn placeholder(&self, pane: Pane) -> &'static str {
        match pane {
            Pane::Request => "No request body.",
            Pane::Response if self.status.is_none() => "Waiting for the local service.",
            Pane::Response => "No response body.",
        }
    }
}

fn draw(frame: &mut ratatui::Frame<'_>, dashboard: &Dashboard) {
    frame.render_widget(
        Block::default().style(Style::default().bg(CANVAS)),
        frame.area(),
    );
    let area = pad(frame.area());
    let rows = dashboard.visible_exchanges().len() as u16 + 1;
    let list_height = rows.clamp(4, (area.height.saturating_sub(6) * 45 / 100).max(4));
    let [header, endpoint, requests, detail, footer] = Layout::vertical([
        Constraint::Length(1),
        Constraint::Length(3),
        Constraint::Length(list_height),
        Constraint::Min(4),
        Constraint::Length(1),
    ])
    .areas(area);

    draw_header(frame, dashboard, header);
    draw_endpoint(frame, dashboard, endpoint);
    draw_requests(frame, dashboard, requests);
    draw_detail(frame, dashboard, detail);
    draw_footer(frame, dashboard, footer);
}

fn pad(area: Rect) -> Rect {
    Rect {
        x: area.x + 1,
        width: area.width.saturating_sub(2),
        ..area
    }
}

fn draw_header(frame: &mut ratatui::Frame<'_>, dashboard: &Dashboard, area: Rect) {
    let (mark, label, color) = if dashboard.online {
        ("●", "ONLINE", ACCENT)
    } else {
        ("◌", "RECONNECTING", Color::Yellow)
    };
    let left = Line::from(vec![
        Span::styled(
            "gnar",
            Style::default().fg(ACCENT).add_modifier(Modifier::BOLD),
        ),
        Span::raw("  "),
        Span::styled(format!("{mark} {label}"), Style::default().fg(color)),
    ]);
    let per_minute = dashboard
        .exchanges
        .iter()
        .filter(|exchange| exchange.started.elapsed() < Duration::from_secs(60))
        .count();
    frame.render_widget(Paragraph::new(left), area);
    frame.render_widget(
        Paragraph::new(Line::styled(
            format!("{per_minute}/min · {} CAPTURED", dashboard.exchanges.len()),
            Style::default().fg(MUTED),
        ))
        .right_aligned(),
        area,
    );
}

fn draw_endpoint(frame: &mut ratatui::Frame<'_>, dashboard: &Dashboard, area: Rect) {
    let lines = vec![
        Line::raw(""),
        Line::from(vec![
            Span::styled("↗  ", Style::default().fg(MUTED)),
            Span::styled(
                dashboard.public_url.as_str(),
                Style::default().fg(LINK).add_modifier(Modifier::BOLD),
            ),
        ]),
        Line::from(vec![
            Span::styled("↘  ", Style::default().fg(MUTED)),
            Span::styled(dashboard.target.as_str(), Style::default().fg(MUTED)),
        ]),
    ];
    frame.render_widget(Paragraph::new(lines), area);
}

fn rule<'a>(title: &str, detail: String) -> Block<'a> {
    Block::default()
        .borders(Borders::TOP)
        .border_style(Style::default().fg(SELECTION))
        .title(Span::styled(
            format!(" {title} "),
            Style::default().fg(MUTED).add_modifier(Modifier::BOLD),
        ))
        .title_top(Line::styled(format!(" {detail} "), Style::default().fg(MUTED)).right_aligned())
}

fn draw_requests(frame: &mut ratatui::Frame<'_>, dashboard: &Dashboard, area: Rect) {
    let visible = dashboard.visible_exchanges();
    let detail = if dashboard.following {
        "FOLLOWING".to_string()
    } else {
        format!("HELD · {} OF {}", dashboard.selected + 1, visible.len())
    };
    let block = rule("REQUESTS", detail);
    if visible.is_empty() {
        let hint = if dashboard.filter.is_empty() {
            "Waiting for the first request."
        } else {
            "No request matches this filter."
        };
        frame.render_widget(
            Paragraph::new(hint)
                .style(Style::default().fg(MUTED))
                .block(block),
            area,
        );
        return;
    }

    let rows = visible.iter().map(|exchange| {
        let method = if exchange.replayed {
            format!("↻ {}", exchange.method)
        } else {
            exchange.method.clone()
        };
        Row::new([
            Cell::from(method).style(Style::default().fg(if exchange.replayed {
                LINK
            } else {
                Color::Reset
            })),
            Cell::from(exchange.path.clone()),
            Cell::from(
                exchange
                    .status
                    .map_or("···".into(), |status| status.to_string()),
            )
            .style(status_style(exchange.status)),
            Cell::from(
                Line::from(
                    exchange
                        .duration_ms
                        .map_or("···".to_string(), |duration| format!("{duration}ms")),
                )
                .right_aligned(),
            )
            .style(Style::default().fg(MUTED)),
        ])
    });
    let table = Table::new(
        rows,
        [
            Constraint::Length(9),
            Constraint::Min(20),
            Constraint::Length(6),
            Constraint::Length(9),
        ],
    )
    .column_spacing(2)
    .row_highlight_style(Style::default().bg(SELECTION).add_modifier(Modifier::BOLD))
    .highlight_symbol("› ")
    .block(block);
    let mut state = TableState::default()
        .with_selected(dashboard.selected.min(visible.len().saturating_sub(1)));
    frame.render_stateful_widget(table, area, &mut state);
}

fn draw_detail(frame: &mut ratatui::Frame<'_>, dashboard: &Dashboard, area: Rect) {
    let Some(exchange) = dashboard.selection() else {
        frame.render_widget(rule("INSPECT", String::new()), area);
        return;
    };

    let (label, other) = match dashboard.pane {
        Pane::Request => ("REQUEST", "tab → response"),
        Pane::Response => ("RESPONSE", "tab → request"),
    };
    let (headers, body) = exchange.pane(dashboard.pane);
    let mut lines = headers
        .iter()
        .map(|(name, value)| {
            let value = if sensitive_header(name) {
                "<redacted>".to_string()
            } else {
                String::from_utf8_lossy(value).into_owned()
            };
            Line::from(vec![
                Span::styled(format!("{name}: "), Style::default().fg(MUTED)),
                Span::raw(value),
            ])
        })
        .collect::<Vec<_>>();
    if let Some(error) = &exchange.error {
        lines.push(Line::raw(""));
        lines.push(Line::styled(
            error.clone(),
            Style::default().fg(Color::LightRed),
        ));
    } else if !body.is_empty() {
        if !headers.is_empty() {
            lines.push(Line::raw(""));
        }
        let text = display_body(headers, body);
        let body_lines = text.lines().collect::<Vec<_>>();
        lines.extend(
            body_lines
                .iter()
                .take(BODY_PREVIEW_LINES)
                .map(|line| Line::raw((*line).to_string())),
        );
        if body_lines.len() > BODY_PREVIEW_LINES {
            lines.push(Line::styled(
                format!(
                    "… {} more lines not shown",
                    body_lines.len() - BODY_PREVIEW_LINES
                ),
                Style::default().fg(MUTED),
            ));
        }
    } else {
        lines.push(Line::styled(
            exchange.placeholder(dashboard.pane),
            Style::default().fg(MUTED),
        ));
    }

    let title = format!(
        "{label}  {} {}",
        exchange.method,
        truncate(&exchange.path, area.width.saturating_sub(28) as usize)
    );
    frame.render_widget(
        Paragraph::new(lines)
            .wrap(Wrap { trim: false })
            .block(rule(&title, other.to_string())),
        area,
    );
}

fn draw_footer(frame: &mut ratatui::Frame<'_>, dashboard: &Dashboard, area: Rect) {
    if dashboard.filter_editing {
        frame.render_widget(
            Paragraph::new(Line::from(vec![
                Span::styled("FILTER ", Style::default().fg(MUTED)),
                Span::styled(
                    format!("{}▌", dashboard.filter),
                    Style::default().fg(ACCENT),
                ),
                Span::styled("   enter apply · esc clear", Style::default().fg(MUTED)),
            ])),
            area,
        );
        return;
    }

    let keys = [
        ("↑↓", "select"),
        ("tab", "req/res"),
        ("/", "filter"),
        ("r", "replay"),
        ("e", "curl"),
        ("c", "copy"),
        ("o", "open"),
        (
            "space",
            if dashboard.following {
                "hold"
            } else {
                "follow"
            },
        ),
        ("q", "quit"),
    ];
    let notice = dashboard.notice.as_ref();
    let reserved = notice.map_or(0, |notice| notice.text.chars().count() as u16 + 2);
    let [keys_area, notice_area] =
        Layout::horizontal([Constraint::Min(0), Constraint::Length(reserved)]).areas(area);

    let mut spans = Vec::with_capacity(keys.len() * 3);
    let mut width = 0;
    for (key, action) in keys {
        let separator = u16::from(!spans.is_empty()) * 3;
        let entry = (key.chars().count() + action.chars().count() + 1) as u16;
        if width + separator + entry > keys_area.width {
            break;
        }
        width += separator + entry;
        if separator > 0 {
            spans.push(Span::styled(" · ", Style::default().fg(SELECTION)));
        }
        spans.push(Span::styled(
            key,
            Style::default().fg(MUTED).add_modifier(Modifier::BOLD),
        ));
        spans.push(Span::styled(
            format!(" {action}"),
            Style::default().fg(MUTED),
        ));
    }
    frame.render_widget(Paragraph::new(Line::from(spans)), keys_area);

    if let Some(notice) = notice {
        let color = if notice.sticky { Color::Yellow } else { ACCENT };
        frame.render_widget(
            Paragraph::new(Line::styled(
                notice.text.as_str(),
                Style::default().fg(color).add_modifier(Modifier::BOLD),
            ))
            .right_aligned(),
            notice_area,
        );
    }
}

fn capture(destination: &mut Vec<u8>, source: &[u8]) {
    let remaining = MAX_CAPTURE_BYTES.saturating_sub(destination.len());
    destination.extend_from_slice(&source[..source.len().min(remaining)]);
}

fn truncate(value: &str, width: usize) -> String {
    if width < 4 || value.chars().count() <= width {
        return value.to_string();
    }
    value.chars().take(width - 1).collect::<String>() + "…"
}

fn status_style(status: Option<u16>) -> Style {
    let color = match status {
        Some(200..=299) => ACCENT,
        Some(300..=399) => LINK,
        Some(400..=499) => Color::Yellow,
        Some(500..) => Color::LightRed,
        _ => MUTED,
    };
    Style::default().fg(color)
}

fn sensitive_header(name: &str) -> bool {
    matches!(
        name.to_ascii_lowercase().as_str(),
        "authorization" | "cookie" | "proxy-authorization" | "set-cookie" | "x-api-key"
    )
}

fn generated_header(name: &str) -> bool {
    matches!(
        name.to_ascii_lowercase().as_str(),
        "host" | "content-length" | "connection" | "transfer-encoding"
    )
}

fn shell_quote(value: &str) -> String {
    format!("'{}'", value.replace('\'', "'\"'\"'"))
}

fn display_body(headers: &[Header], body: &[u8]) -> String {
    body_text(headers, body).unwrap_or_else(|| format!("<binary body · {} bytes>", body.len()))
}

fn body_text(headers: &[Header], body: &[u8]) -> Option<String> {
    let content_type = headers
        .iter()
        .find(|(name, _)| name.eq_ignore_ascii_case("content-type"))
        .map(|(_, value)| String::from_utf8_lossy(value).to_ascii_lowercase())
        .unwrap_or_default();
    let textual = content_type.is_empty()
        || content_type.starts_with("text/")
        || ["json", "xml", "javascript", "x-www-form-urlencoded"]
            .iter()
            .any(|kind| content_type.contains(kind));
    if !textual {
        return None;
    }
    let text = std::str::from_utf8(body).ok()?;
    if content_type.contains("json")
        && let Ok(mut value) = serde_json::from_str::<serde_json::Value>(text)
    {
        redact_json(&mut value);
        return serde_json::to_string_pretty(&value).ok();
    }
    Some(text.to_string())
}

fn redact_json(value: &mut serde_json::Value) {
    match value {
        serde_json::Value::Object(fields) => {
            for (name, value) in fields {
                if sensitive_field(name) {
                    *value = serde_json::Value::String("<redacted>".into());
                } else {
                    redact_json(value);
                }
            }
        }
        serde_json::Value::Array(values) => {
            for value in values {
                redact_json(value);
            }
        }
        _ => {}
    }
}

fn sensitive_field(name: &str) -> bool {
    matches!(
        name.to_ascii_lowercase().replace('-', "_").as_str(),
        "access_token"
            | "api_key"
            | "authorization"
            | "client_secret"
            | "password"
            | "refresh_token"
            | "secret"
            | "token"
    )
}

fn open_url(url: &str) -> io::Result<()> {
    #[cfg(target_os = "macos")]
    let mut command = Command::new("open");
    #[cfg(target_os = "linux")]
    let mut command = Command::new("xdg-open");
    #[cfg(target_os = "windows")]
    let mut command = {
        let mut command = Command::new("cmd");
        command.args(["/C", "start", ""]);
        command
    };
    command.arg(url).spawn().map(|_| ())
}

#[cfg(test)]
mod tests {
    use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
    use ratatui::Terminal;
    use ratatui::backend::TestBackend;

    use super::{Dashboard, Intent, Pane, View, display_body, draw, edge_lines, sensitive_header};
    use crate::protocol::{ClientFrame, EdgeFrame};

    fn dashboard() -> Dashboard {
        Dashboard::new(
            "https://example.test".into(),
            "http://127.0.0.1:3000".into(),
        )
    }

    #[test]
    fn edge_selection_uses_a_bounded_window() {
        let edges = (1..=8)
            .map(|index| format!("https://edge-{index}.example.com"))
            .collect::<Vec<_>>();
        let mut view = View::new(edges.len());

        let first = edge_lines(&edges, &view);
        assert_eq!(first.len(), 7);
        assert!(first[0].contains("edge-1.example.com"));
        assert!(first[6].contains(&super::PROMPT_FADED.to_string()));

        view.select(7);
        let last = edge_lines(&edges, &view);
        assert!(last[0].contains(&super::PROMPT_FADED.to_string()));
        assert!(last[6].contains("edge-8.example.com"));
    }

    fn press(dashboard: &mut Dashboard, code: KeyCode) -> Intent {
        dashboard.key(KeyEvent::new(code, KeyModifiers::NONE))
    }

    fn request(dashboard: &mut Dashboard, id: u64, method: &str, path: &str) {
        dashboard.apply_edge(&EdgeFrame::RequestStart {
            id,
            method: method.into(),
            path: path.into(),
            headers: vec![],
        });
    }

    #[test]
    fn newest_request_stays_selected_while_following() {
        let mut dashboard = dashboard();

        request(&mut dashboard, 1, "GET", "/first");
        request(&mut dashboard, 2, "GET", "/second");

        assert_eq!(dashboard.selection().unwrap().path, "/second");
        assert!(dashboard.following);
    }

    #[test]
    fn down_walks_toward_older_requests_and_holds_the_list() {
        let mut dashboard = dashboard();
        request(&mut dashboard, 1, "GET", "/older");
        request(&mut dashboard, 2, "GET", "/newer");

        press(&mut dashboard, KeyCode::Down);

        assert_eq!(dashboard.selection().unwrap().path, "/older");
        assert!(!dashboard.following);

        request(&mut dashboard, 3, "GET", "/newest");
        assert_eq!(dashboard.selection().unwrap().path, "/older");

        press(&mut dashboard, KeyCode::Up);
        press(&mut dashboard, KeyCode::Up);
        assert_eq!(dashboard.selection().unwrap().path, "/newest");
        assert!(dashboard.following);
    }

    #[test]
    fn selection_never_leaves_the_visible_list() {
        let mut dashboard = dashboard();
        request(&mut dashboard, 1, "GET", "/only");

        for _ in 0..5 {
            press(&mut dashboard, KeyCode::Down);
        }

        assert_eq!(dashboard.selected, 0);
        assert_eq!(dashboard.selection().unwrap().path, "/only");
    }

    #[test]
    fn tab_switches_pane() {
        let mut dashboard = dashboard();

        press(&mut dashboard, KeyCode::Tab);

        assert!(dashboard.pane == Pane::Request);
    }

    #[test]
    fn action_keys_are_inert_while_filtering() {
        let mut dashboard = dashboard();
        request(&mut dashboard, 1, "GET", "/health");

        press(&mut dashboard, KeyCode::Char('/'));
        for code in "req".chars().map(KeyCode::Char) {
            assert!(matches!(press(&mut dashboard, code), Intent::None));
        }

        assert_eq!(dashboard.filter, "req");
        assert!(dashboard.visible_exchanges().is_empty());

        press(&mut dashboard, KeyCode::Esc);
        assert!(dashboard.filter.is_empty());
        assert_eq!(dashboard.visible_exchanges().len(), 1);
    }

    #[test]
    fn filter_matches_method_path_and_status() {
        let mut dashboard = dashboard();
        request(&mut dashboard, 1, "GET", "/health");
        dashboard.apply_client(&ClientFrame::Start {
            id: 1,
            status: 503,
            headers: vec![],
        });

        for filter in ["get", "health", "503"] {
            dashboard.filter = filter.into();
            assert_eq!(dashboard.visible_exchanges().len(), 1, "{filter}");
        }
        dashboard.filter = "missing".into();
        assert!(dashboard.visible_exchanges().is_empty());
    }

    #[test]
    fn quit_keys_report_quit() {
        assert!(matches!(
            press(&mut dashboard(), KeyCode::Char('q')),
            Intent::Quit
        ));
        assert!(matches!(
            dashboard().key(KeyEvent::new(KeyCode::Char('c'), KeyModifiers::CONTROL)),
            Intent::Quit
        ));
        assert!(matches!(
            press(&mut dashboard(), KeyCode::Char('c')),
            Intent::CopyUrl
        ));
    }

    #[test]
    fn transient_notice_expires_but_offline_notice_persists() {
        let mut dashboard = dashboard();
        dashboard.notice = Some(super::Notice::new("copied"));
        dashboard.notice.as_mut().unwrap().shown -= super::NOTICE_LIFETIME;

        assert!(dashboard.expire_notice());
        assert!(dashboard.notice.is_none());

        dashboard.notice = Some(super::Notice::sticky("edge disconnected, reconnecting"));
        dashboard.notice.as_mut().unwrap().shown -= super::NOTICE_LIFETIME;

        assert!(!dashboard.expire_notice());
        assert!(dashboard.notice.is_some());
    }

    #[test]
    fn replay_is_a_new_local_exchange() {
        let mut dashboard = dashboard();
        request(&mut dashboard, 1, "POST", "/webhook");
        dashboard.apply_edge(&EdgeFrame::RequestChunk {
            id: 1,
            body: b"payload".to_vec(),
        });
        press(&mut dashboard, KeyCode::Down);

        let replay = dashboard.begin_replay().unwrap();

        assert_eq!(replay.method, "POST");
        assert_eq!(replay.body, b"payload");
        assert!(dashboard.exchanges[0].replayed);
        assert!(dashboard.following);
        assert_eq!(dashboard.selected, 0);
    }

    fn sample_rows(count: usize) -> Vec<super::ServiceRow> {
        let services = [
            ("Next.js", ":3000", "Acme Checkout"),
            ("Vite", ":5173", "Infer Lab · 手点一遍 Transformer"),
            ("Ollama", ":11434", "ollama"),
            ("JSON API", ":9090", "mihomo"),
            ("Express", ":14122", "Sub Store"),
            ("Gunicorn", ":8000", "internal API · HTTP 404"),
            ("web app", ":4173", "preview build"),
            ("Flask", ":5001", "webhook sink"),
            ("JSON API", ":38324", "Clash Party"),
            ("nginx", ":8080", "static site"),
        ];
        services
            .iter()
            .take(count)
            .map(|(kind, origin, detail)| super::ServiceRow {
                kind: format!("{kind:<8}"),
                origin: origin.to_string(),
                detail: detail.to_string(),
            })
            .collect()
    }

    #[test]
    #[ignore = "visual inspection only"]
    fn prompt_snapshot() {
        for (total, selected) in [(4, 1), (10, 0), (10, 4), (10, 9)] {
            let rows = sample_rows(total);
            let mut view = super::View::new(rows.len());
            view.select(selected);

            println!("── {total} services, selection {} ──", selected + 1);
            print!("{}", super::prompt_header(rows.len()));
            for line in view.lines(&rows) {
                println!("{line}");
            }
            println!("{}", view.hint());
            println!();
        }
    }

    #[test]
    fn login_choices_mark_only_the_selected_row() {
        let anonymous = super::choice_row(0, "Anyone may publish", "no accounts", true);
        let account = super::choice_row(1, "Require an account", "accounts", false);

        assert!(anonymous.contains('›'));
        assert!(anonymous.contains(&super::PROMPT_ACCENT.to_string()));
        assert!(!account.contains('›'));
        assert!(account.contains('2'));
    }

    #[test]
    fn short_list_shows_every_service_without_scrolling() {
        let rows = sample_rows(4);
        let view = super::View::new(rows.len());

        assert!(!view.scrolls());
        assert_eq!(view.lines(&rows).len(), 4);
        assert!(view.hint().contains("1-4 jump"));
        assert!(!view.hint().contains(" of "));
    }

    #[test]
    fn long_list_keeps_a_fixed_window_and_reports_position() {
        let rows = sample_rows(10);
        let mut view = super::View::new(rows.len());

        assert!(view.scrolls());
        assert_eq!(view.lines(&rows).len(), super::MAX_VISIBLE);
        assert!(view.hint().contains("1-9 jump"));
        assert!(view.hint().contains("1 of 10"));

        view.select(9);
        assert_eq!(view.lines(&rows).len(), super::MAX_VISIBLE);
        assert_eq!(view.hidden_below(), 0);
        assert!(view.hint().contains("10 of 10"));
    }

    #[test]
    fn window_follows_selection_and_keeps_a_lookahead_margin() {
        let rows = sample_rows(10);
        let mut view = super::View::new(rows.len());

        view.select(5);
        assert_eq!(view.offset, 0, "selection already sits inside the window");

        view.select(6);
        assert!(view.offset > 0, "window scrolls to keep a row in reserve");
        for index in 0..rows.len() {
            view.select(index);
            let visible = view.offset..view.offset + view.height;
            assert!(
                visible.contains(&index),
                "selection {index} must be visible"
            );
            assert_eq!(view.lines(&rows).len(), view.height);
        }
    }

    #[test]
    fn edge_rows_fade_only_where_content_is_hidden() {
        let rows = sample_rows(10);
        let mut view = super::View::new(rows.len());

        let lines = view.lines(&rows);
        let faded = super::PROMPT_FADED.to_string();
        assert!(!lines[0].contains(&faded), "nothing hidden above");
        assert!(lines[super::MAX_VISIBLE - 1].contains(&faded));

        view.select(9);
        let lines = view.lines(&rows);
        assert!(lines[0].contains(&faded), "rows hidden above");
        assert!(!lines[super::MAX_VISIBLE - 1].contains(&faded));
    }

    #[test]
    fn header_counts_exactly_what_the_list_shows() {
        assert_eq!(super::prompt_header(1), "Found 1 local service\n");
        assert_eq!(super::prompt_header(10), "Found 10 local services\n");
    }

    #[test]
    fn credential_headers_are_sensitive() {
        assert!(sensitive_header("Authorization"));
        assert!(sensitive_header("cookie"));
        assert!(!sensitive_header("content-type"));
    }

    #[test]
    fn curl_export_omits_credentials_and_generated_headers() {
        let mut dashboard = dashboard();
        dashboard.apply_edge(&EdgeFrame::RequestStart {
            id: 1,
            method: "POST".into(),
            path: "/hook".into(),
            headers: vec![
                ("authorization".into(), b"Bearer secret".to_vec()),
                ("content-length".into(), b"11".to_vec()),
                ("content-type".into(), b"application/json".to_vec()),
            ],
        });
        dashboard.apply_edge(&EdgeFrame::RequestChunk {
            id: 1,
            body: br#"{"ok":true}"#.to_vec(),
        });

        let command = dashboard.export_curl().unwrap();

        assert!(command.contains("--request POST"));
        assert!(command.contains("https://example.test/hook"));
        assert!(command.contains("application/json"));
        assert!(!command.contains("secret"));
        assert!(!command.contains("content-length"));
    }

    #[test]
    fn json_secrets_are_redacted_from_display_and_export() {
        let headers = vec![("content-type".into(), b"application/json".to_vec())];
        let body = br#"{"token":"top-level","profile":{"password":"nested"},"clients":[{"client_secret":"array"}],"safe":"visible"}"#;

        let displayed = display_body(&headers, body);

        assert!(displayed.contains("<redacted>"));
        assert!(displayed.contains("visible"));
        assert!(!displayed.contains("top-level"));
        assert!(!displayed.contains("nested"));
        assert!(!displayed.contains("array"));

        let mut dashboard = dashboard();
        dashboard.apply_edge(&EdgeFrame::RequestStart {
            id: 1,
            method: "POST".into(),
            path: "/hook".into(),
            headers,
        });
        dashboard.apply_edge(&EdgeFrame::RequestChunk {
            id: 1,
            body: body.to_vec(),
        });

        let command = dashboard.export_curl().unwrap();

        assert!(command.contains("<redacted>"));
        assert!(command.contains("visible"));
        assert!(!command.contains("top-level"));
        assert!(!command.contains("nested"));
        assert!(!command.contains("array"));
    }

    fn render(dashboard: &Dashboard) -> String {
        let mut terminal = Terminal::new(TestBackend::new(100, 30)).unwrap();
        terminal.draw(|frame| draw(frame, dashboard)).unwrap();
        terminal
            .backend()
            .buffer()
            .content
            .iter()
            .map(|cell| cell.symbol())
            .collect()
    }

    #[test]
    #[ignore = "visual inspection only"]
    fn snapshot() {
        let mut dashboard = dashboard();
        for (id, method, path, status, body) in [
            (1u64, "GET", "/api/users", 200u16, r#"{"users":[]}"#),
            (2, "POST", "/webhooks/github", 500, r#"{"error":"boom"}"#),
            (3, "GET", "/health", 200, "ok"),
        ] {
            request(&mut dashboard, id, method, path);
            dashboard.apply_client(&ClientFrame::Start {
                id,
                status,
                headers: vec![
                    ("content-type".into(), b"application/json".to_vec()),
                    ("authorization".into(), b"Bearer nope".to_vec()),
                ],
            });
            dashboard.apply_client(&ClientFrame::Chunk {
                id,
                body: body.as_bytes().to_vec(),
            });
            dashboard.apply_client(&ClientFrame::End { id });
        }
        for id in 4..18u64 {
            request(&mut dashboard, id, "GET", &format!("/assets/chunk-{id}.js"));
            dashboard.apply_client(&ClientFrame::Start {
                id,
                status: 200,
                headers: vec![],
            });
            dashboard.apply_client(&ClientFrame::End { id });
        }
        dashboard.notice = Some(super::Notice::new("public URL copied"));

        let mut terminal = Terminal::new(TestBackend::new(88, 26)).unwrap();
        terminal.draw(|frame| draw(frame, &dashboard)).unwrap();
        let buffer = terminal.backend().buffer();
        println!("┌{}┐", "─".repeat(88));
        for row in 0..buffer.area.height {
            let line: String = (0..buffer.area.width)
                .map(|column| buffer[(column, row)].symbol())
                .collect();
            println!("│{line}│");
        }
        println!("└{}┘", "─".repeat(88));
    }

    #[test]
    fn dashboard_renders_endpoint_and_live_exchange() {
        let mut dashboard = dashboard();
        request(&mut dashboard, 1, "GET", "/api/users");
        dashboard.apply_client(&ClientFrame::Start {
            id: 1,
            status: 200,
            headers: vec![("content-type".into(), b"application/json".to_vec())],
        });
        dashboard.apply_client(&ClientFrame::Chunk {
            id: 1,
            body: br#"{"users":[]}"#.to_vec(),
        });
        dashboard.apply_client(&ClientFrame::End { id: 1 });

        let screen = render(&dashboard);

        assert!(screen.contains("gnar"));
        assert!(screen.contains("ONLINE"));
        assert!(screen.contains("CAPTURED"));
        assert!(screen.contains("FOLLOWING"));
        assert!(screen.contains("https://example.test"));
        assert!(screen.contains("http://127.0.0.1:3000"));
        assert!(screen.contains("/api/users"));
        assert!(screen.contains("200"));
        assert!(screen.contains("RESPONSE"));
        assert!(screen.contains("users"));
        assert!(screen.contains("q quit"));
    }

    #[test]
    fn long_bodies_only_show_the_first_twelve_lines() {
        let mut dashboard = dashboard();
        request(&mut dashboard, 1, "GET", "/long-page");
        dashboard.apply_client(&ClientFrame::Start {
            id: 1,
            status: 200,
            headers: vec![("content-type".into(), b"text/plain".to_vec())],
        });
        let body = (0..30)
            .map(|line| format!("body-line-{line:02}"))
            .collect::<Vec<_>>()
            .join("\n");
        dashboard.apply_client(&ClientFrame::Chunk {
            id: 1,
            body: body.into_bytes(),
        });
        dashboard.apply_client(&ClientFrame::End { id: 1 });

        let screen = render(&dashboard);
        assert!(screen.contains("body-line-00"));
        assert!(screen.contains("body-line-11"));
        assert!(!screen.contains("body-line-12"));
        assert!(!screen.contains("body-line-29"));
        assert!(screen.contains("18 more lines not shown"));
    }

    #[test]
    fn empty_and_offline_states_stay_legible() {
        let mut dashboard = dashboard();
        let screen = render(&dashboard);
        assert!(screen.contains("Waiting for the first request."));

        dashboard.online = false;
        dashboard.notice = Some(super::Notice::sticky("edge disconnected, reconnecting"));
        let screen = render(&dashboard);
        assert!(screen.contains("RECONNECTING"));

        dashboard.filter = "nothing".into();
        let screen = render(&dashboard);
        assert!(screen.contains("No request matches this filter."));
    }
}
