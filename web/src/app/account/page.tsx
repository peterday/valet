"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type CreateAccountKeyRequest } from "@/lib/api";
import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";

export default function AccountKeysPage() {
  const queryClient = useQueryClient();
  const { data: keys, isLoading } = useQuery({
    queryKey: ["account-keys"],
    queryFn: api.listAccountKeys,
  });

  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<CreateAccountKeyRequest>({
    label: "",
    env_var: "",
    provider: "",
    value: "",
  });

  const createMutation = useMutation({
    mutationFn: api.createAccountKey,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["account-keys"] });
      setShowForm(false);
      setForm({ label: "", env_var: "", provider: "", value: "" });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: api.deleteAccountKey,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["account-keys"] });
    },
  });

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-semibold">Account Keys</h2>
        <button
          onClick={() => setShowForm(!showForm)}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-[var(--primary)] text-[var(--primary-foreground)] rounded-md hover:opacity-90"
        >
          <Plus size={14} />
          Add Key
        </button>
      </div>

      {showForm && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createMutation.mutate(form);
          }}
          className="border border-[var(--border)] rounded-lg p-4 mb-6 space-y-3"
        >
          <div className="grid grid-cols-2 gap-3">
            <input
              placeholder="Label (e.g. openai-personal)"
              value={form.label}
              onChange={(e) => setForm({ ...form, label: e.target.value })}
              className="px-3 py-2 border border-[var(--border)] rounded-md bg-[var(--background)] text-sm"
              required
            />
            <input
              placeholder="Env var (e.g. OPENAI_API_KEY)"
              value={form.env_var}
              onChange={(e) => setForm({ ...form, env_var: e.target.value })}
              className="px-3 py-2 border border-[var(--border)] rounded-md bg-[var(--background)] text-sm"
              required
            />
            <input
              placeholder="Provider (e.g. openai)"
              value={form.provider}
              onChange={(e) => setForm({ ...form, provider: e.target.value })}
              className="px-3 py-2 border border-[var(--border)] rounded-md bg-[var(--background)] text-sm"
            />
            <input
              placeholder="Value (e.g. sk-abc123...)"
              value={form.value}
              onChange={(e) => setForm({ ...form, value: e.target.value })}
              className="px-3 py-2 border border-[var(--border)] rounded-md bg-[var(--background)] text-sm"
              type="password"
              required
            />
          </div>
          <div className="flex gap-2">
            <button
              type="submit"
              disabled={createMutation.isPending}
              className="px-3 py-1.5 text-sm bg-[var(--primary)] text-[var(--primary-foreground)] rounded-md hover:opacity-90 disabled:opacity-50"
            >
              {createMutation.isPending ? "Saving..." : "Save"}
            </button>
            <button
              type="button"
              onClick={() => setShowForm(false)}
              className="px-3 py-1.5 text-sm border border-[var(--border)] rounded-md hover:bg-[var(--muted)]"
            >
              Cancel
            </button>
          </div>
          {createMutation.isError && (
            <p className="text-sm text-[var(--destructive)]">
              {createMutation.error.message}
            </p>
          )}
        </form>
      )}

      {isLoading ? (
        <p className="text-sm text-[var(--muted-foreground)]">Loading...</p>
      ) : keys?.length === 0 ? (
        <p className="text-sm text-[var(--muted-foreground)]">
          No account keys yet.
        </p>
      ) : (
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-[var(--border)] text-left text-[var(--muted-foreground)]">
              <th className="pb-2 font-medium">Label</th>
              <th className="pb-2 font-medium">Env Var</th>
              <th className="pb-2 font-medium">Provider</th>
              <th className="pb-2 font-medium">Created</th>
              <th className="pb-2 w-10"></th>
            </tr>
          </thead>
          <tbody>
            {keys?.map((key) => (
              <tr
                key={key.id}
                className="border-b border-[var(--border)] hover:bg-[var(--muted)]"
              >
                <td className="py-2.5 font-medium">{key.label}</td>
                <td className="py-2.5 font-mono text-xs">{key.env_var}</td>
                <td className="py-2.5">
                  {key.provider ? (
                    <span className="px-2 py-0.5 bg-[var(--muted)] rounded text-xs">
                      {key.provider}
                    </span>
                  ) : (
                    <span className="text-[var(--muted-foreground)]">-</span>
                  )}
                </td>
                <td className="py-2.5 text-[var(--muted-foreground)]">
                  {new Date(key.created_at).toLocaleDateString()}
                </td>
                <td className="py-2.5">
                  <button
                    onClick={() => deleteMutation.mutate(key.label)}
                    className="p-1 text-[var(--muted-foreground)] hover:text-[var(--destructive)] transition-colors"
                  >
                    <Trash2 size={14} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
