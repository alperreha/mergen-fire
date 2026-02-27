import { execFile } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

const DEFAULT_IMAGES_DIR = "/var/lib/mergen/images";
const DEFAULT_CONVERTER_TIMEOUT_MS = 20 * 60 * 1000;
const MAX_BUFFER_BYTES = 20 * 1024 * 1024;

let activeConversion = null;

export class ConverterBusyError extends Error {
  constructor(active) {
    super(`converter is already running for image: ${active?.image ?? "unknown"}`);
    this.name = "ConverterBusyError";
    this.activeConversion = active;
  }
}

export class ConverterExecError extends Error {
  constructor(message, details) {
    super(message);
    this.name = "ConverterExecError";
    this.details = details;
  }
}

function normalizeImageName(raw) {
  if (typeof raw !== "string") {
    return "";
  }
  return raw.trim();
}

function getImagesBaseDir() {
  const fromEnv = process.env.MERGEN_IMAGES_DIR?.trim();
  return fromEnv || DEFAULT_IMAGES_DIR;
}

function getRepoRoot() {
  const fromEnv = process.env.MERGEN_REPO_ROOT?.trim();
  if (fromEnv) {
    return fromEnv;
  }
  return path.resolve(process.cwd(), "..", "..");
}

function getConverterCommand() {
  const customBinary = process.env.MERGEN_CONVERTER_BIN?.trim();
  if (customBinary) {
    return {
      command: customBinary,
      baseArgs: [],
      cwd: getRepoRoot(),
    };
  }
  return {
    command: "go",
    baseArgs: ["run", "./cmd/mergen-converter"],
    cwd: getRepoRoot(),
  };
}

async function pathExists(target) {
  try {
    await fs.access(target);
    return true;
  } catch {
    return false;
  }
}

async function readJSONIfExists(target) {
  try {
    const body = await fs.readFile(target, "utf-8");
    return JSON.parse(body);
  } catch (error) {
    if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") {
      return null;
    }
    return null;
  }
}

async function findConvertedImageDirs(rootDir) {
  const result = [];

  async function walk(current) {
    let entries;
    try {
      entries = await fs.readdir(current, { withFileTypes: true });
    } catch (error) {
      if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") {
        return;
      }
      throw error;
    }

    const hasMeta = entries.some(entry => entry.isFile() && entry.name === "image-meta.json");
    if (hasMeta) {
      result.push(current);
      return;
    }

    for (const entry of entries) {
      if (!entry.isDirectory() || entry.name.startsWith(".")) {
        continue;
      }
      await walk(path.join(current, entry.name));
    }
  }

  await walk(rootDir);
  result.sort((a, b) => a.localeCompare(b));
  return result;
}

function toISODate(rawDate) {
  if (!(rawDate instanceof Date)) {
    return new Date().toISOString();
  }
  if (Number.isNaN(rawDate.getTime())) {
    return new Date().toISOString();
  }
  return rawDate.toISOString();
}

function extractValueByPrefix(output, prefix) {
  const lines = output.split(/\r?\n/);
  for (const line of lines) {
    if (line.startsWith(prefix)) {
      return line.slice(prefix.length).trim();
    }
  }
  return "";
}

function asImageRecordName(relativePath, metadata) {
  if (metadata && typeof metadata.image === "string" && metadata.image.trim()) {
    return metadata.image.trim();
  }
  return relativePath;
}

async function buildImageRecord(baseDir, outputDir) {
  const relativePath = path.relative(baseDir, outputDir) || path.basename(outputDir);
  const metadataPath = path.join(outputDir, "image-meta.json");
  const suggestedPath = path.join(outputDir, "suggested-vm-request.json");

  const [metaJSON, suggestedJSON, stats, hasRootfs, hasAgent, hasPayload, hasEnv, hasSuggested] =
    await Promise.all([
      readJSONIfExists(metadataPath),
      readJSONIfExists(suggestedPath),
      fs.stat(outputDir),
      pathExists(path.join(outputDir, "golden-rootfs.ext4")),
      pathExists(path.join(outputDir, "agent-rootfs.ext4")),
      pathExists(path.join(outputDir, "payload-rootfs.ext4")),
      pathExists(path.join(outputDir, "env-rootfs.ext4")),
      pathExists(suggestedPath),
    ]);

  return {
    id: relativePath,
    image: asImageRecordName(relativePath, metaJSON),
    outputDir,
    updatedAt: toISODate(stats?.mtime),
    ready: hasRootfs && hasAgent && hasPayload && hasEnv,
    artifacts: {
      rootfs: hasRootfs,
      agentDisk: hasAgent,
      payloadDisk: hasPayload,
      envDisk: hasEnv,
      suggestedVM: hasSuggested,
    },
    paths: {
      rootfs: path.join(outputDir, "golden-rootfs.ext4"),
      agentDisk: path.join(outputDir, "agent-rootfs.ext4"),
      payloadDisk: path.join(outputDir, "payload-rootfs.ext4"),
      envDisk: path.join(outputDir, "env-rootfs.ext4"),
      suggestedVM: suggestedPath,
    },
    suggestedRequest: suggestedJSON,
  };
}

export async function listConvertedImages() {
  const baseDir = getImagesBaseDir();
  const dirs = await findConvertedImageDirs(baseDir);
  const items = await Promise.all(dirs.map(dir => buildImageRecord(baseDir, dir)));

  items.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));

  return {
    baseDir,
    total: items.length,
    items,
    activeConversion,
  };
}

export async function runConversion(imageInput) {
  const image = normalizeImageName(imageInput);
  if (!image) {
    throw new Error("image is required");
  }

  if (activeConversion) {
    throw new ConverterBusyError(activeConversion);
  }

  const timeoutMs = Number(process.env.MERGEN_CONVERTER_TIMEOUT_MS || DEFAULT_CONVERTER_TIMEOUT_MS);
  const { command, baseArgs, cwd } = getConverterCommand();
  const args = [...baseArgs, "-image", image];
  activeConversion = {
    image,
    startedAt: new Date().toISOString(),
  };

  try {
    const { stdout, stderr } = await execFileAsync(command, args, {
      cwd,
      timeout: timeoutMs,
      maxBuffer: MAX_BUFFER_BYTES,
      env: process.env,
    });

    const outputDir = extractValueByPrefix(stdout, "output dir:");

    return {
      image,
      outputDir,
      stdout,
      stderr,
    };
  } catch (error) {
    const details = {
      message: error?.message || "converter command failed",
      stdout: typeof error?.stdout === "string" ? error.stdout : "",
      stderr: typeof error?.stderr === "string" ? error.stderr : "",
      code: typeof error?.code === "number" ? error.code : null,
      signal: typeof error?.signal === "string" ? error.signal : null,
    };
    throw new ConverterExecError(`converter command failed for ${image}`, details);
  } finally {
    activeConversion = null;
  }
}
