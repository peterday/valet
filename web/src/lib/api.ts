const BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:9876";

async function fetchAPI<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
  });

  if (!res.ok) {
    const error = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(error.error || res.statusText);
  }

  if (res.status === 204) return undefined as T;
  return res.json();
}

export const api = {
  // Account keys
  listAccountKeys: () => fetchAPI<AccountKey[]>("/api/v1/account-keys"),
  createAccountKey: (data: CreateAccountKeyRequest) =>
    fetchAPI<AccountKey>("/api/v1/account-keys", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  deleteAccountKey: (label: string) =>
    fetchAPI<void>(`/api/v1/account-keys/${encodeURIComponent(label)}`, {
      method: "DELETE",
    }),

  // Projects
  listProjects: () => fetchAPI<Project[]>("/api/v1/projects"),
  createProject: (data: CreateProjectRequest) =>
    fetchAPI<Project>("/api/v1/projects", {
      method: "POST",
      body: JSON.stringify(data),
    }),

  // Bindings
  listBindings: (slug: string, env: string) =>
    fetchAPI<Binding[]>(`/api/v1/projects/${slug}/environments/${env}/bindings`),
  createBinding: (slug: string, env: string, data: CreateBindingRequest) =>
    fetchAPI<Binding>(
      `/api/v1/projects/${slug}/environments/${env}/bindings`,
      { method: "POST", body: JSON.stringify(data) }
    ),
  deleteBinding: (slug: string, env: string, envVar: string) =>
    fetchAPI<void>(
      `/api/v1/projects/${slug}/environments/${env}/bindings/${encodeURIComponent(envVar)}`,
      { method: "DELETE" }
    ),

  // Export
  exportEnv: (slug: string, env: string, format?: string) =>
    fetchAPI<Record<string, string>>(
      `/api/v1/projects/${slug}/environments/${env}/export?format=json`
    ),
};

// Types
export interface AccountKey {
  id: string;
  label: string;
  env_var: string;
  provider: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface Project {
  id: string;
  name: string;
  slug: string;
  created_at: string;
}

export interface Binding {
  id: string;
  project_id: string;
  environment_id: string;
  env_var: string;
  account_key_id?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateAccountKeyRequest {
  label: string;
  env_var: string;
  provider: string;
  value: string;
}

export interface CreateProjectRequest {
  name: string;
  slug: string;
}

export interface CreateBindingRequest {
  env_var: string;
  account_key_id?: string;
  value?: string;
}
