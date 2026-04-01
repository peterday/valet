"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { KeyRound, FolderOpen, AlertCircle } from "lucide-react";
import Link from "next/link";

export default function DashboardPage() {
  const { data: keys } = useQuery({
    queryKey: ["account-keys"],
    queryFn: api.listAccountKeys,
  });
  const { data: projects } = useQuery({
    queryKey: ["projects"],
    queryFn: api.listProjects,
  });

  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Dashboard</h2>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-8">
        <div className="border border-[var(--border)] rounded-lg p-4">
          <div className="flex items-center gap-2 text-[var(--muted-foreground)] mb-1">
            <KeyRound size={16} />
            <span className="text-sm">Account Keys</span>
          </div>
          <p className="text-2xl font-semibold">{keys?.length ?? 0}</p>
        </div>
        <div className="border border-[var(--border)] rounded-lg p-4">
          <div className="flex items-center gap-2 text-[var(--muted-foreground)] mb-1">
            <FolderOpen size={16} />
            <span className="text-sm">Projects</span>
          </div>
          <p className="text-2xl font-semibold">{projects?.length ?? 0}</p>
        </div>
      </div>

      {keys?.length === 0 && projects?.length === 0 && (
        <div className="border border-[var(--border)] rounded-lg p-6 text-center">
          <AlertCircle
            size={32}
            className="mx-auto mb-3 text-[var(--muted-foreground)]"
          />
          <h3 className="font-medium mb-1">Get started</h3>
          <p className="text-sm text-[var(--muted-foreground)] mb-4">
            Add your first API key or create a project using the CLI.
          </p>
          <code className="text-sm bg-[var(--muted)] px-3 py-1.5 rounded">
            valet add OPENAI_API_KEY sk-... --account openai
          </code>
        </div>
      )}

      {projects && projects.length > 0 && (
        <div>
          <h3 className="font-medium mb-3">Projects</h3>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {projects.map((p) => (
              <Link
                key={p.id}
                href={`/projects/detail?slug=${p.slug}`}
                className="border border-[var(--border)] rounded-lg p-4 hover:border-[var(--foreground)] transition-colors"
              >
                <p className="font-medium">{p.name}</p>
                <p className="text-sm text-[var(--muted-foreground)]">
                  {p.slug}
                </p>
              </Link>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
