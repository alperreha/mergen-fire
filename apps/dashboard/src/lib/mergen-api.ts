type Dict = Record<string, unknown>;

export type PortBinding = {
  guest: number;
  host: number;
  protocol: string;
};

export type VMSummary = {
  id: string;
  createdAt: string;
  systemd: {
    available: boolean;
    active: boolean;
    activeState?: string;
    subState?: string;
    unit?: string;
    mainPID?: number;
  };
  firecracker: {
    socketPath?: string;
    socketPresent: boolean;
  };
  network: {
    guestIP: string;
    ports: PortBinding[];
    tapName: string;
    netns: string;
  };
  metadata?: Dict;
  paths?: Dict;
};

export type VMListResponse = {
  items: VMSummary[];
};

export type VMMetadata = {
  id: string;
  createdAt: string;
  rootfs: string;
  kernel: string;
  agentDisk: string;
  payloadDisk: string;
  envDisk: string;
  dataDisk?: string;
  httpPort?: number;
  guestIP: string;
  metadata?: Dict;
  tags?: Record<string, string>;
  ports: PortBinding[];
  paths?: Dict;
};

export type VMConfig = Dict;
export type VMHooks = Dict;

export type CreateVMRequest = {
  rootfs: string;
  kernel: string;
  agentDisk: string;
  payloadDisk: string;
  envDisk: string;
  dataDisk?: string;
  vcpu: number;
  memMiB: number;
  httpPort?: number;
  autoStart?: boolean;
  bootArgs?: string;
  metadata?: Dict;
  tags?: Record<string, string>;
  ports?: Array<{
    guest: number;
    host: number;
    protocol?: string;
  }>;
};

export type VMActionResponse = {
  id: string;
  status: string;
};

// Browser -> same-origin /api/v1/* -> SolidStart server -> mergen daemon (:8080)
const rawAPIBase = (import.meta.env.VITE_MERGEN_API_BASE as string | undefined)?.trim() || "";
const API_BASE = /^https?:\/\//i.test(rawAPIBase) ? "/api" : rawAPIBase || "/api";

function apiPath(path: string): string {
  return `${API_BASE}${path}`;
}

async function parseError(response: Response): Promise<string> {
  const raw = await response.text();
  if (!raw) {
    return `HTTP ${response.status} ${response.statusText}`;
  }

  try {
    const payload = JSON.parse(raw) as { message?: string };
    if (typeof payload.message === "string" && payload.message.trim()) {
      return payload.message;
    }
  } catch {
    // fall through
  }

  return `HTTP ${response.status} ${response.statusText}: ${raw}`;
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers ?? {});
  if (init?.body && !headers.has("content-type")) {
    headers.set("content-type", "application/json");
  }

  const response = await fetch(apiPath(path), {
    ...init,
    headers,
  });

  if (!response.ok) {
    throw new Error(await parseError(response));
  }

  return (await response.json()) as T;
}

export function listVMs(): Promise<VMListResponse> {
  return requestJSON<VMListResponse>("/v1/vms");
}

export function getVM(id: string): Promise<VMSummary> {
  return requestJSON<VMSummary>(`/v1/vms/${encodeURIComponent(id)}`);
}

export function getVMMeta(id: string): Promise<VMMetadata> {
  return requestJSON<VMMetadata>(`/v1/vms/${encodeURIComponent(id)}/meta.json`);
}

export function getVMConfig(id: string): Promise<VMConfig> {
  return requestJSON<VMConfig>(`/v1/vms/${encodeURIComponent(id)}/vm.json`);
}

export function getVMHooks(id: string): Promise<VMHooks> {
  return requestJSON<VMHooks>(`/v1/vms/${encodeURIComponent(id)}/hooks.json`);
}

export function createVM(payload: CreateVMRequest): Promise<VMActionResponse> {
  return requestJSON<VMActionResponse>("/v1/vms", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function startVM(id: string): Promise<VMActionResponse> {
  return requestJSON<VMActionResponse>(`/v1/vms/${encodeURIComponent(id)}/start`, {
    method: "POST",
    body: "{}",
  });
}

export function stopVM(id: string): Promise<VMActionResponse> {
  return requestJSON<VMActionResponse>(`/v1/vms/${encodeURIComponent(id)}/stop`, {
    method: "POST",
    body: "{}",
  });
}

export function deleteVM(id: string, retainData = false): Promise<VMActionResponse> {
  const query = retainData ? "?retainData=true" : "";
  return requestJSON<VMActionResponse>(`/v1/vms/${encodeURIComponent(id)}${query}`, {
    method: "DELETE",
  });
}
