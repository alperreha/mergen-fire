type Dict = Record<string, unknown>;

export type FlatRow = {
  key: string;
  value: string;
};

export function valueToText(value: unknown): string {
  if (value === null) return "null";
  if (value === undefined) return "undefined";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

export function flattenObject(value: unknown): FlatRow[] {
  const rows: FlatRow[] = [];

  const walk = (node: unknown, keyPath: string) => {
    if (Array.isArray(node)) {
      if (node.length === 0) {
        rows.push({ key: keyPath || "(root)", value: "[]" });
        return;
      }
      node.forEach((entry, index) => {
        const next = keyPath ? `${keyPath}[${index}]` : `[${index}]`;
        walk(entry, next);
      });
      return;
    }

    if (node !== null && typeof node === "object") {
      const entries = Object.entries(node as Dict);
      if (entries.length === 0) {
        rows.push({ key: keyPath || "(root)", value: "{}" });
        return;
      }
      entries.forEach(([key, entry]) => {
        const next = keyPath ? `${keyPath}.${key}` : key;
        walk(entry, next);
      });
      return;
    }

    rows.push({ key: keyPath || "(root)", value: valueToText(node) });
  };

  walk(value, "");
  return rows;
}

export function formatTimestamp(raw: string | undefined): string {
  if (!raw) return "-";
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return raw;
  return date.toLocaleString();
}

export function toPortSummary(ports: Array<{ host: number; guest: number; protocol?: string }>): string {
  if (!ports || ports.length === 0) {
    return "-";
  }
  return ports
    .map(port => `${port.host}->${port.guest}/${(port.protocol || "tcp").toLowerCase()}`)
    .join(", ");
}
