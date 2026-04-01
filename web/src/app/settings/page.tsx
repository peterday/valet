"use client";

import { Settings } from "lucide-react";

export default function SettingsPage() {
  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Settings</h2>

      <div className="space-y-6">
        <div className="border border-[var(--border)] rounded-lg p-4">
          <h3 className="font-medium mb-2">Encryption</h3>
          <p className="text-sm text-[var(--muted-foreground)] mb-3">
            Your vault is encrypted with an auto-generated key stored at{" "}
            <code className="bg-[var(--muted)] px-1.5 py-0.5 rounded text-xs">
              ~/.valet/key
            </code>
          </p>
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center gap-1 text-xs text-[var(--success)]">
              <Settings size={12} />
              Encryption active
            </span>
          </div>
        </div>

        <div className="border border-[var(--border)] rounded-lg p-4">
          <h3 className="font-medium mb-2">Data Location</h3>
          <div className="space-y-1 text-sm">
            <p>
              <span className="text-[var(--muted-foreground)]">Database:</span>{" "}
              <code className="bg-[var(--muted)] px-1.5 py-0.5 rounded text-xs">
                ~/.valet/valet.db
              </code>
            </p>
            <p>
              <span className="text-[var(--muted-foreground)]">Key:</span>{" "}
              <code className="bg-[var(--muted)] px-1.5 py-0.5 rounded text-xs">
                ~/.valet/key
              </code>
            </p>
          </div>
        </div>

        <div className="border border-[var(--border)] rounded-lg p-4 opacity-50">
          <h3 className="font-medium mb-2">Master Password</h3>
          <p className="text-sm text-[var(--muted-foreground)]">
            Optionally protect your encryption key with a master password.
          </p>
          <p className="text-xs text-[var(--muted-foreground)] mt-2">
            Coming soon
          </p>
        </div>
      </div>
    </div>
  );
}
