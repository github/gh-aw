const sound: CracktroDeck.Sound = {
  muted: false,
  toggle: () => true,
  cut() {},
  techno(_level) {},
  slide(_direction, _music, _shift) {},
  touch() {},
  step(_direction) {},
};

const controller = CracktroDeck.create({
  slides: ({ h, letters, resetIndex, sound, current }) => {
    resetIndex();
    sound.touch();
    return [
      {
        group: "example",
        el: h("section", { class: "slide" }, letters("Example")),
        onKey: event => event.key === "x" && current()?.group === "example",
      },
    ];
  },
  themes: { example: { red: true, prog: [0, 5] } },
  bands: { example: ["Example", "A reusable deck"] },
  sound,
});
controller.go(0);
const kioskOn: boolean = controller.kiosk(true);
controller.kiosk();
void kioskOn;
controller.rows[0].items[0].el.focus();
const currentIndex: number = controller.current();
void currentIndex;

CracktroDeck.create({ slides: [{ group: "plain", el: document.createElement("section") }] });
// @ts-expect-error slides need a group
CracktroDeck.create({ slides: [{ el: document.createElement("section") }] });
// @ts-expect-error band labels require a title and subtitle
CracktroDeck.create({ bands: { example: ["Example"] } });
// @ts-expect-error the runtime requires the touch method
CracktroDeck.create({ sound: { ...sound, touch: undefined as string } });
// @ts-expect-error navigation takes a numeric slide index
controller.go("1");
