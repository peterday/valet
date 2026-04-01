"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import Link from "next/link";
import { FolderOpen } from "lucide-react";

export default function ProjectsPage() {
  const { data: projects, isLoading } = useQuery({
    queryKey: ["projects"],
    queryFn: api.listProjects,
  });

  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Projects</h2>

      {isLoading ? (
        <p className="text-sm text-[var(--muted-foreground)]">Loading...</p>
      ) : projects?.length === 0 ? (
        <div className="text-center py-8">
          <FolderOpen
            size={32}
            className="mx-auto mb-3 text-[var(--muted-foreground)]"
          />
          <p className="text-sm text-[var(--muted-foreground)] mb-3">
            No projects yet.
          </p>
          <code className="text-sm bg-[var(--muted)] px-3 py-1.5 rounded">
            cd your-project && valet init
          </code>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {projects?.map((p) => (
            <Link
              key={p.id}
              href={`/projects/detail?slug=${p.slug}`}
              className="border border-[var(--border)] rounded-lg p-4 hover:border-[var(--foreground)] transition-colors"
            >
              <div className="flex items-center gap-2 mb-2">
                <FolderOpen size={16} className="text-[var(--muted-foreground)]" />
                <p className="font-medium">{p.name}</p>
              </div>
              <p className="text-sm text-[var(--muted-foreground)]">{p.slug}</p>
              <p className="text-xs text-[var(--muted-foreground)] mt-2">
                Created {new Date(p.created_at).toLocaleDateString()}
              </p>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
