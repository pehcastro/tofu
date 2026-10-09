import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync, existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const desk = path.resolve(here, "../..");
const packs = path.resolve(process.argv[2] ?? path.join(desk, "../../.local/desk-app/icons/editor"));
const out = path.join(desk, "crates/ui/assets/editor-icons");
const glyphFill = "#c5c5ce";

const jsonc = (file) =>
  JSON.parse(
    readFileSync(file, "utf8")
      .replace(/^\s*\/\/.*$/gm, "")
      .replace(/,(\s*[}\]])/g, "$1"),
  );
const load = async (file) => import(pathToFileURL(file).href);
const svg = (file) => {
  if (!existsSync(file)) throw new Error(`no svg at ${file}`);
  return readFileSync(file, "utf8").replace(/<\?xml[^>]*\?>/, "").replace(/<!--[\s\S]*?-->/g, "").replace(/>\s+</g, "><").trim();
};
const iconPixels = 16;
const errorPixels = 0.02;
const number = /-?(?:\d+\.?\d*|\.\d+)(?:e[-+]?\d+)?/gi;
const short = (value, digits) => {
  const text = (+Number(value).toFixed(digits)).toString();
  return text.replace(/^(-?)0\./, "$1.").replace(/^-0$/, "0");
};
const hex = (percent) => Math.round(Math.min(100, +percent) * 2.55).toString(16).padStart(2, "0");
const minified = (body) => {
  const box = body.match(/viewBox="([^"]+)"/)?.[1].trim().split(/[\s,]+/).map(Number) ?? [0, 0, iconPixels, iconPixels];
  const units = Math.max(box[2], box[3]);
  const digits = Math.max(0, Math.ceil(Math.log10(iconPixels / (units * errorPixels))));
  const numbers = (text) => text.replace(number, (value) => short(value, digits));
  const used = new Set([...body.matchAll(/(?:url\(#|href="#)([^)"]+)/g)].map((match) => match[1]));
  let text = body
    .replace(/<(metadata|title|desc|sodipodi:namedview)\b[\s\S]*?<\/\1>/g, "")
    .replace(/<sodipodi:namedview\b[^>]*\/>/g, "")
    .replace(/\s(?:sodipodi|inkscape):[\w-]+="[^"]*"/g, "")
    .replace(/\s(?:xmlns:(?:sodipodi|inkscape|dc|cc|rdf|svg)|version|xml:space|data-name)="[^"]*"/g, "")
    .replace(/\sid="([^"]*)"/g, (all, id) => (used.has(id) ? all : ""))
    .replace(/rgb\(\s*([\d.]+)%\s*,\s*([\d.]+)%\s*,\s*([\d.]+)%\s*\)/g, (_, r, g, b) => `#${hex(r)}${hex(g)}${hex(b)}`)
    .replace(/\s(d|points|transform|x|y|x1|x2|y1|y2|cx|cy|r|rx|ry|stroke-width|offset)="([^"]*)"/g, (_, name, value) => ` ${name}="${numbers(value)}"`)
    .replace(/\sd="([^"]*)"/g, (_, value) => ` d="${value.replace(/\s*([a-zA-Z])\s*/g, "$1").replace(/[\s,]+/g, " ").replace(/ -/g, "-").trim()}"`)
    .replace(/style="\s*([^"]*)"/g, (_, style) => {
      const kept = style.split(";").map((rule) => rule.trim()).filter((rule) => rule && !/^(fill-opacity|stroke-opacity|opacity):\s*1$/.test(rule) && rule !== "fill-rule:nonzero");
      return kept.length ? `style="${kept.join(";")}"` : "";
    })
    .replace(/<g>((?:(?!<g\b)[\s\S])*?)<\/g>/g, "$1")
    .replace(/\s+(\/?>)/g, "$1")
    .replace(/\s{2,}/g, " ");
  if (!text.includes("xlink:")) text = text.replace(/\sxmlns:xlink="[^"]*"/, "");
  if (!text.includes("<style")) text = text.replace(/\sstyle="([^"]*)"/g, (all, style) => attributes(style) ?? all);
  if (!/stroke(?!:none|="none")/.test(text)) text = text.replace(/\sstroke="none"/g, "");
  return text;
};
const presentation = new Set(["fill", "stroke", "stroke-width", "fill-rule", "clip-rule", "stroke-linecap", "stroke-linejoin", "opacity", "fill-opacity", "stroke-opacity"]);
const attributes = (style) => {
  const rules = style.split(";").filter(Boolean).map((rule) => rule.split(":").map((part) => part.trim()));
  if (!rules.every(([name, value]) => presentation.has(name) && value !== undefined)) return undefined;
  return rules.map(([name, value]) => ` ${name}="${value}"`).join("");
};
const sized = (body) =>
  body.replace(/<svg\b[^>]*>/, (root) => {
    const size = (name) => root.match(new RegExp(`\\s${name}="([\\d.]+)(px)?"`))?.[1];
    const box = /\sviewBox=/.test(root) ? "" : ` viewBox="0 0 ${size("width") ?? iconPixels} ${size("height") ?? iconPixels}"`;
    const bare = root.replace(/\s(width|height)="[^"]*"/g, "");
    return bare.replace("<svg", `<svg width="${iconPixels}" height="${iconPixels}"${box}`);
  });
const lower = (table = {}) => Object.fromEntries(Object.entries(table).map(([key, id]) => [key.toLowerCase(), id]));

function normalized(name, license, theme, draw) {
  const pack = {
    name,
    license,
    file: theme.file,
    folder: theme.folder,
    ...(theme.folderExpanded && { folderOpen: theme.folderExpanded }),
    fileNames: lower(theme.fileNames),
    fileExtensions: lower(theme.fileExtensions),
    languageIds: theme.languageIds ?? {},
    folderNames: lower(theme.folderNames),
    folderNamesOpen: lower(theme.folderNamesExpanded),
  };
  const lookups = ["fileNames", "fileExtensions", "languageIds", "folderNames", "folderNamesOpen"];
  const used = new Set([pack.file, pack.folder, pack.folderOpen].concat(...lookups.map((key) => Object.values(pack[key]))).filter(Boolean));
  const drawn = Object.fromEntries([...used].sort().flatMap((id) => (draw(id) ? [[id, minified(sized(draw(id)))]] : [])));
  for (const id of [pack.file, pack.folder, pack.folderOpen].filter(Boolean)) {
    if (!drawn[id]) throw new Error(`${name} has no svg for its default ${id}`);
  }
  const first = new Map();
  const canonical = Object.fromEntries(Object.entries(drawn).map(([id, body]) => [id, first.get(body) ?? (first.set(body, id), id)]));
  for (const key of ["file", "folder", "folderOpen"].filter((key) => pack[key])) pack[key] = canonical[pack[key]];
  for (const key of lookups) {
    const dropped = Object.entries(pack[key]).filter(([, id]) => !drawn[id]);
    for (const [entry] of dropped) delete pack[key][entry];
    if (dropped.length) console.log(`${name}: dropped ${key} ${dropped.map(([entry, id]) => `${entry}=${id}`).join(" ")}, the pack defines no such icon`);
    for (const [entry, id] of Object.entries(pack[key])) pack[key][entry] = canonical[id];
  }
  pack.icons = Object.fromEntries([...first].map(([body, id]) => [id, body]));
  const deduped = Object.keys(drawn).length - first.size;
  if (deduped) console.log(`${name}: ${deduped} icons were byte-identical to another and now share its id`);
  return pack;
}

const vscode = (manifest) => {
  const theme = jsonc(manifest);
  return [
    theme,
    (id) => {
      const definition = theme.iconDefinitions[id];
      return definition?.iconPath && svg(path.join(path.dirname(manifest), definition.iconPath));
    },
  ];
};

async function catppuccin(root) {
  const files = await load(path.join(root, "src/defaults/fileIcons.ts"));
  const folders = await load(path.join(root, "src/defaults/folderIcons.ts"));
  const open = Object.fromEntries(Object.entries(folders.folderNames).map(([name, id]) => [name, `${id}_open`]));
  const theme = {
    file: "_file",
    folder: "_folder",
    folderExpanded: "_folder_open",
    fileNames: files.fileNames,
    fileExtensions: files.fileExtensions,
    languageIds: files.languageIds,
    folderNames: folders.folderNames,
    folderNamesExpanded: open,
  };
  return [theme, (id) => svg(path.join(root, "icons/mocha", `${id}.svg`))];
}

async function pierre(root) {
  const { fill } = await load(path.join(root, "scripts/palette.mjs"));
  const tier = async (name) => (await load(path.join(root, "scripts/themes", `${name}.mjs`))).default;
  const icons = [...(await tier("minimal")), ...(await tier("default")), ...(await tier("complete"))];
  const byName = new Map(icons.map((icon) => [icon.name, icon]));
  const theme = { file: "file-duo", folder: "folder-duo", folderExpanded: "folder-open-duo", fileNames: {}, fileExtensions: {}, folderNames: {}, folderNamesExpanded: {} };
  for (const icon of icons) {
    for (const extension of icon.fileExtensions ?? []) theme.fileExtensions[extension] = icon.name;
    for (const name of icon.fileNames ?? []) theme.fileNames[name] = icon.name;
    for (const name of icon.folderNames ?? []) theme.folderNames[name] = theme.folderNamesExpanded[name] = icon.name;
  }
  const draw = (id) => {
    const icon = byName.get(id);
    let body = svg(path.join(root, "svgs", `${icon.svgName ?? icon.name}.svg`));
    if (icon.color?.fg) {
      body = body.replaceAll("currentColor", icon.color.fg.dark).replace(">", `><style>.bg{fill:${icon.color.bg.dark}}</style>`);
    } else {
      body = body.replaceAll("currentColor", icon.color?.dark ?? fill.dark);
    }
    return icon.opacity && icon.opacity !== 1 ? body.replace("<svg", `<svg opacity="${icon.opacity}"`) : body;
  };
  return [theme, draw];
}

function phosphor(root) {
  const manifest = path.join(root, "themes/ph-file-icon-theme.json");
  const theme = jsonc(manifest);
  const font = path.join(path.dirname(manifest), theme.fonts[0].src[0].path);
  const points = Object.fromEntries(
    Object.entries(theme.iconDefinitions).map(([id, definition]) => [id, definition.fontCharacter.replace(/^\\+/, "")]),
  );
  const drawn = JSON.parse(
    execFileSync("python", [path.join(here, "glyphs.py"), font, glyphFill, ...new Set(Object.values(points))], { encoding: "utf8" }),
  );
  return [
    theme,
    (id) => {
      const glyph = drawn[points[id]];
      if (!glyph) throw new Error(`phosphor has no glyph ${points[id]} for ${id}`);
      return glyph;
    },
  ];
}

const material = path.join(desk, "crates/ui/assets/files/material.json");
const sources = [
  ["material", "Material", "MIT, Material Icon Theme, bundled before DESK-186", () => vscode(material)],
  ["catppuccin", "Catppuccin Mocha", "MIT, Catppuccin 2023", () => catppuccin(path.join(packs, "catpuccin"))],
  ["github", "GitHub", "MIT, Juwan Petty 2022", () => vscode(path.join(packs, "github/fileicons/github-icons-theme.json"))],
  ["jetbrains", "JetBrains", "MIT, fogio-org 2025", () => vscode(path.join(packs, "jetbrains/themes/dark-jetbrains-icon-theme.json"))],
  ["makinda", "Makinda Stroke", "MIT, Makinda Jackson", () => vscode(path.join(packs, "makinda/themes/makinda-file-icon-theme.stroke.json"))],
  ["phosphor", "Phosphor", "MIT, Phosphor Icons 2024", () => phosphor(path.join(packs, "phosphor"))],
  ["pierre", "Pierre", "MIT, The Pierre Computer Company 2026", () => pierre(path.join(packs, "pierre"))],
  ["symbols", "Symbols", "MIT, Miguel Solorio 2020-22", () => vscode(path.join(packs, "symbols/src/symbol-icon-theme.json"))],
];

const fileBudget = 400_000;
const shards = (pack) => {
  const files = [{ ...pack, icons: {} }];
  let size = Buffer.byteLength(JSON.stringify(files[0]));
  for (const [id, body] of Object.entries(pack.icons)) {
    const entry = Buffer.byteLength(JSON.stringify({ [id]: body }));
    if (size + entry > fileBudget) {
      files.push({ icons: {} });
      size = Buffer.byteLength(JSON.stringify(files.at(-1)));
    }
    files.at(-1).icons[id] = body;
    size += entry;
  }
  return files.map((file) => `${JSON.stringify(file)}\n`);
};

mkdirSync(out, { recursive: true });
for (const stale of readdirSync(out).filter((file) => file.endsWith(".json"))) rmSync(path.join(out, stale));
for (const [key, name, license, read] of sources) {
  const [theme, draw] = await read();
  const pack = normalized(name, license, theme, draw);
  const texts = shards(pack);
  const sizes = texts.map((text, at) => {
    const file = at === 0 ? `${key}.json` : `${key}-${at + 1}.json`;
    const bytes = Buffer.byteLength(text);
    if (bytes > fileBudget) throw new Error(`${file} is ${bytes} bytes, over ${fileBudget}`);
    writeFileSync(path.join(out, file), text);
    return `${file} ${(bytes / 1000).toFixed(0)} KB`;
  });
  const count = (table) => Object.keys(pack[table]).length;
  console.log(
    `${key}: ${Object.keys(pack.icons).length} icons, ${count("fileNames")} names, ${count("fileExtensions")} extensions, ${count("languageIds")} languages, ${count("folderNames")} folders, ${count("folderNamesOpen")} open folders; ${sizes.join(", ")}`,
  );
}
