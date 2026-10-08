use desk_ui::components::card::inner_card;
use desk_ui::components::paint::{ink, ring};
use desk_ui::theme::Theme;
use gpui::{ClickEvent, Context, Div, Stateful, div, prelude::*, px, relative, rgb};

use super::fixture::{LEDGER, LEDGER_FIRST_ID, POINTS, Point, TELL_CHANGE_MODEL, Verdict};
use super::kit::{
    BASE, GAP, Spark, T2, T3, WELL, button, cap, ellipsis, faint, figure, fraction, headline, mono,
    panes, pill, shade, shell, shell_head, spark,
};
use super::{Classifier, Page, mode_colors, verdict_colors};

const COLUMNS: [f32; 4] = [44.0, 64.0, 56.0, 56.0];
const SUBJECT_COLUMN: f32 = 120.0;
const TABLE_LEAST: f32 = 380.0;
const WHY_LEAST: f32 = 340.0;
const CARD_LEAST: f32 = 180.0;
const ROW_ON: f32 = 0.07;
const CARD_RING: f32 = 0.25;
const MODEL_RING: f32 = 0.07;
const BAR_ON: f32 = 0.75;
const BAR_OFF: f32 = 0.08;
const LABEL_ON: f32 = 0.88;
const LABEL_ON_TEXT: u32 = 0x111111;
const BARS: usize = 5;

impl Classifier {
    pub(super) fn ledger(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let model = div()
            .id("change-model")
            .flex()
            .flex_wrap()
            .items_center()
            .gap(px(GAP))
            .min_h(px(32.0))
            .pl(px(12.0))
            .pr(px(6.0))
            .rounded(px(9.0))
            .cursor_pointer()
            .bg(shade(theme, WELL))
            .shadow(vec![ring(ink(theme, MODEL_RING))])
            .text_size(px(12.5))
            .on_click(cx.listener(Self::tell(TELL_CHANGE_MODEL)))
            .child(faint("model", 12.5, T3, theme))
            .child(div().font_family(mono(theme)).child("jev-latest"))
            .child(faint("2026-09-30", 11.5, T3, theme).font_family(mono(theme)))
            .child(div().w(px(1.0)).h(px(14.0)).bg(ink(theme, 0.1)))
            .child(faint("via", 12.5, T3, theme))
            .child(div().child("openrouter"))
            .child(faint("key ····88c0", 11.5, T3, theme).font_family(mono(theme)))
            .child(
                faint("change", 11.5, T3, theme)
                    .py(px(3.0))
                    .px(px(8.0))
                    .rounded(px(7.0))
                    .line_height(relative(1.0))
                    .bg(ink(theme, 0.06)),
            );
        let top = headline("Classifier")
            .min_h(px(32.0))
            .child(faint(
                "decides at fixed points in the loop · this project, 7 days",
                13.0,
                T3,
                theme,
            ))
            .child(div().flex_1())
            .child(model)
            .child(
                button("sandbox", "Threshold sandbox", 32.0, theme).on_click(cx.listener(
                    |this, _: &ClickEvent, _, cx| {
                        this.page = Page::Sandbox;
                        cx.notify();
                    },
                )),
            );
        let cards = div().flex().flex_none().flex_wrap().gap(px(GAP)).children(
            POINTS.iter().enumerate().map(|(index, point)| {
                fraction(self.point_card(index, point, theme, cx), 1.0, CARD_LEAST)
            }),
        );
        let main = panes()
            .child(fraction(self.table(theme, cx), 3.0, TABLE_LEAST))
            .child(fraction(self.why(theme, cx), 2.0, WHY_LEAST));
        div().child(top).child(cards).child(main)
    }

    fn point_card(
        &self,
        index: usize,
        point: &'static Point,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let line = Spark {
            values: &point.spark,
            width: 70.0,
            height: 20.0,
            floor: 19.0,
            rise: 18.0,
            stroke: ink(theme, 0.55),
            area: None,
            dashed: false,
            dot: Some(rgb(0xffffff)),
        };
        shell(theme)
            .id(("point", index))
            .flex_1()
            .cursor_pointer()
            .when(index == self.point, |card| {
                card.shadow(vec![ring(ink(theme, CARD_RING))])
            })
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.point = index;
                this.row = 0;
                cx.notify();
            }))
            .child(
                shell_head(theme)
                    .h(px(32.0))
                    .child(cap(point.name, 10.0, theme).flex_1())
                    .child(
                        pill(
                            if point.enforced { "enforced" } else { "shadow" },
                            mode_colors(point.enforced, theme),
                            11.0,
                            7.0,
                        )
                        .py(px(1.0)),
                    ),
            )
            .child(
                inner_card(theme)
                    .py(px(10.0))
                    .px(px(12.0))
                    .gap(px(4.0))
                    .child(
                        div()
                            .flex()
                            .items_end()
                            .gap(px(GAP))
                            .child(figure(point.count, 22.0, 24.2).flex_1())
                            .child(spark(line)),
                    )
                    .child(ellipsis(faint(point.sub, 11.5, T3, theme))),
            )
    }

    fn table(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let [at, verdict, value, label] = COLUMNS;
        let header = div()
            .flex()
            .py(px(6.0))
            .px(px(10.0))
            .child(fraction(cap("at", 10.0, theme), 1.0, at))
            .child(fraction(cap("verdict", 10.0, theme), 1.0, verdict))
            .child(fraction(cap("subject", 10.0, theme), 4.0, SUBJECT_COLUMN))
            .child(fraction(cap("value", 10.0, theme), 1.0, value))
            .child(fraction(cap("label", 10.0, theme), 1.0, label));
        let rows = LEDGER.iter().enumerate().map(|(index, entry)| {
            let tag = self
                .labels
                .get(index)
                .copied()
                .flatten()
                .map_or("-", Verdict::name);
            div()
                .id(("entry", index))
                .flex()
                .items_center()
                .h(px(38.0))
                .px(px(10.0))
                .rounded(px(8.0))
                .cursor_pointer()
                .text_size(px(12.0))
                .when(index == self.row, |row| row.bg(ink(theme, ROW_ON)))
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    this.row = index;
                    cx.notify();
                }))
                .child(fraction(faint(entry.at, 12.5, T3, theme), 1.0, at).font_family(mono(theme)))
                .child(
                    fraction(div(), 1.0, verdict).flex().child(
                        pill(
                            entry.verdict.name(),
                            verdict_colors(entry.verdict),
                            11.0,
                            7.0,
                        )
                        .line_height(px(16.0)),
                    ),
                )
                .child(
                    fraction(div(), 4.0, SUBJECT_COLUMN)
                        .truncate()
                        .font_family(mono(theme))
                        .child(entry.subject),
                )
                .child(
                    fraction(faint(entry.value, 12.0, T2, theme), 1.0, value)
                        .truncate()
                        .font_family(mono(theme)),
                )
                .child(fraction(faint(tag, 12.0, T3, theme), 1.0, label).truncate())
        });
        let point = POINTS.get(self.point).map_or("", |point| point.name);
        shell(theme)
            .child(
                shell_head(theme)
                    .child(cap(format!("Ledger · {point}"), 10.0, theme).flex_1())
                    .child(ellipsis(faint(
                        "every verdict, with its reason",
                        12.0,
                        T3,
                        theme,
                    ))),
            )
            .child(inner_card(theme).p(px(6.0)).child(header).children(rows))
    }

    fn why(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let row = self.row;
        let Some(entry) = LEDGER.get(row) else {
            return shell(theme);
        };
        let chosen = self.labels.get(row).copied().flatten();
        let answers = entry.answers.iter().map(|(question, value)| {
            let lit = value
                .parse::<f32>()
                .map_or(0, |share| (share * BARS as f32).round() as usize);
            div()
                .child(
                    div()
                        .flex()
                        .text_size(px(12.5))
                        .child(faint(*question, 12.5, T2, theme).flex_1())
                        .child(div().font_family(mono(theme)).child(*value)),
                )
                .child(
                    div()
                        .flex()
                        .gap(px(2.0))
                        .mt(px(5.0))
                        .children((0..BARS).map(|bar| {
                            div()
                                .flex_1()
                                .h(px(6.0))
                                .rounded(px(2.0))
                                .bg(ink(theme, if bar < lit { BAR_ON } else { BAR_OFF }))
                        })),
                )
        });
        let labels = Verdict::ALL.map(|verdict| {
            button(verdict.name(), verdict.name(), 28.0, theme)
                .when(chosen == Some(verdict), |on| {
                    on.bg(ink(theme, LABEL_ON)).text_color(rgb(LABEL_ON_TEXT))
                })
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    if let Some(slot) = this.labels.get_mut(row) {
                        *slot = Some(verdict);
                    }
                    cx.notify();
                }))
        });
        shell(theme)
            .child(
                shell_head(theme)
                    .child(cap("Why", 10.0, theme).flex_1())
                    .child(
                        faint(format!("ledger#{}", LEDGER_FIRST_ID + row), 11.5, T3, theme)
                            .font_family(mono(theme)),
                    ),
            )
            .child(
                inner_card(theme)
                    .pt(px(14.0))
                    .pb(px(30.0))
                    .px(px(16.0))
                    .gap(px(12.0))
                    .text_size(px(13.0))
                    .text_color(ink(theme, BASE))
                    .child(
                        div()
                            .font_family(mono(theme))
                            .text_size(px(12.5))
                            .py(px(8.0))
                            .px(px(10.0))
                            .rounded(px(8.0))
                            .bg(shade(theme, WELL))
                            .child(entry.subject),
                    )
                    .child(cap("Answers", 10.0, theme))
                    .children(answers)
                    .child(
                        faint(entry.reason, 12.5, T2, theme)
                            .line_height(px(19.0))
                            .py(px(10.0))
                            .px(px(12.0))
                            .rounded(px(9.0))
                            .bg(shade(theme, WELL)),
                    )
                    .child(div().flex_1())
                    .child(cap("Was it right?", 10.0, theme))
                    .child(
                        div()
                            .flex()
                            .flex_wrap()
                            .gap(px(6.0))
                            .children(labels)
                            .child(
                                faint(
                                    "writes a label; thresholds tune against labels",
                                    11.5,
                                    T3,
                                    theme,
                                )
                                .self_center(),
                            ),
                    ),
            )
    }
}
