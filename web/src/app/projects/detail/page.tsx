"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useState, Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { Plus, Trash2, KeyRound, FileText, ArrowLeft } from "lucide-react";
import Link from "next/link";

const ENVIRONMENTS = ["local", "dev", "staging", "prod"];

function ProjectDetail() {
  const searchParams = useSearchParams();
  const slug = searchParams.get("slug") || "";
  const queryClient = useQueryClient();
  const [activeEnv, setActiveEnv] = useState("local");
  const [showAddForm, setShowAddForm] = useState(false);
  const [newEnvVar, setNewEnvVar] = useState("");
  const [newValue, setNewValue] = useState("");

  const { data: bindings, isLoading } = useQuery({
    queryKey: ["bindings", slug, activeEnv],
    queryFn: () => api.listBindings(slug, activeEnv),
    enabled: !!slug,
  });

  const { data: accountKeys } = useQuery({
    queryKey: ["account-keys"],
    queryFn: api.listAccountKeys,
  });

  const createMutation = useMutation({
    mutationFn: (data: { env_var: string; value: string }) =>
      api.createBinding(slug, activeEnv, {
        env_var: data.env_var,
        value: data.value,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["bindings", slug, activeEnv],
      });
      setShowAddForm(false);
      setNewEnvVar("");
      setNewValue("");
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (envVar: string) =>
      api.deleteBinding(slug, activeEnv, envVar),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["bindings", slug, activeEnv],
      });
    },
  });

  if (!slug) {
    return <p>No project specified.</p>;
  }

  return (
    <div>
      <div className="flex items-center gap-3 mb-6">
        <Link
          href="/projects"
          className="p-1 text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
        >
          <ArrowLeft size={18} />
        </Link>
        <div className="flex-1">
          <h2 className="text-2xl font-semibold">{slug}</h2>
          <p className="text-sm text-[var(--muted-foreground)]">
            {bindings?.length ?? 0} secrets
          </p>
        </div>
        <button
          onClick={() => setShowAddForm(!showAddForm)}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-[var(--primary)] text-[var(--primary-foreground)] rounded-md hover:opacity-90"
        >
          <Plus size={14} />
          Add Secret
        </button>
      </div>

      {/* Environment switcher */}
      <div className="flex gap-1 mb-6 p-1 bg-[var(--muted)] rounded-lg w-fit">
        {ENVIRONMENTS.map((env) => (
          <button
            key={env}
            onClick={() => setActiveEnv(env)}
            className={`px-3 py-1.5 text-sm rounded-md transition-colors ${
              activeEnv === env
                ? "bg-[var(--background)] font-medium shadow-sm"
                : "text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
            }`}
          >
            {env}
          </button>
        ))}
      </div>

      {showAddForm && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createMutation.mutate({ env_var: newEnvVar, value: newValue });
          }}
          className="border border-[var(--border)] rounded-lg p-4 mb-6 flex gap-3 items-end"
        >
          <div className="flex-1">
            <label className="text-xs text-[var(--muted-foreground)] mb-1 block">
              Key
            </label>
            <input
              placeholder="DATABASE_URL"
              value={newEnvVar}
              onChange={(e) => setNewEnvVar(e.target.value)}
              className="w-full px-3 py-2 border border-[var(--border)] rounded-md bg-[var(--background)] text-sm font-mono"
              required
            />
          </div>
          <div className="flex-1">
            <label className="text-xs text-[var(--muted-foreground)] mb-1 block">
              Value
            </label>
            <input
              placeholder="postgres://..."
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
              className="w-full px-3 py-2 border border-[var(--border)] rounded-md bg-[var(--background)] text-sm"
              type="password"
              required
            />
          </div>
          <button
            type="submit"
            disabled={createMutation.isPending}
            className="px-3 py-2 text-sm bg-[var(--primary)] text-[var(--primary-foreground)] rounded-md hover:opacity-90 disabled:opacity-50"
          >
            Add
          </button>
        </form>
      )}

      {isLoading ? (
        <p className="text-sm text-[var(--muted-foreground)]">Loading...</p>
      ) : bindings?.length === 0 ? (
        <div className="text-center py-8 border border-[var(--border)] rounded-lg">
          <FileText
            size={32}
            className="mx-auto mb-3 text-[var(--muted-foreground)]"
          />
          <p className="text-sm text-[var(--muted-foreground)]">
            No secrets in {activeEnv}
          </p>
        </div>
      ) : (
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-[var(--border)] text-left text-[var(--muted-foreground)]">
              <th className="pb-2 font-medium">Key</th>
              <th className="pb-2 font-medium">Source</th>
              <th className="pb-2 font-medium">Updated</th>
              <th className="pb-2 w-10"></th>
            </tr>
          </thead>
          <tbody>
            {bindings?.map((b) => {
              const accountKey = b.account_key_id
                ? accountKeys?.find((ak) => ak.id === b.account_key_id)
                : null;
              return (
                <tr
                  key={b.id}
                  className="border-b border-[var(--border)] hover:bg-[var(--muted)]"
                >
                  <td className="py-2.5 font-mono text-xs font-medium">
                    {b.env_var}
                  </td>
                  <td className="py-2.5">
                    {accountKey ? (
                      <span className="flex items-center gap-1.5">
                        <KeyRound
                          size={12}
                          className="text-[var(--muted-foreground)]"
                        />
                        <span className="text-xs">
                          account:{accountKey.label}
                        </span>
                      </span>
                    ) : (
                      <span className="text-xs text-[var(--muted-foreground)]">
                        project secret
                      </span>
                    )}
                  </td>
                  <td className="py-2.5 text-[var(--muted-foreground)]">
                    {new Date(b.updated_at).toLocaleDateString()}
                  </td>
                  <td className="py-2.5">
                    <button
                      onClick={() => deleteMutation.mutate(b.env_var)}
                      className="p-1 text-[var(--muted-foreground)] hover:text-[var(--destructive)] transition-colors"
                    >
                      <Trash2 size={14} />
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}

export default function ProjectDetailPage() {
  return (
    <Suspense fallback={<p className="text-sm text-[var(--muted-foreground)]">Loading...</p>}>
      <ProjectDetail />
    </Suspense>
  );
}
