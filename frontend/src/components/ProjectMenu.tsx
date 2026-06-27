import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, Pencil, Trash2 } from "lucide-react";
import { Button } from "./ui/button";
import { useI18n } from "../i18n";
import { ProjectMeta } from "../types";

interface Props {
  projects: ProjectMeta[];
  activeId: string;
  busy: boolean;
  onSwitch: (id: string) => void;
  onRename: (id: string) => void;
  onDelete: (id: string) => void;
}

export default function ProjectMenu({ projects, activeId, busy, onSwitch, onRename, onDelete }: Props) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const active = projects.find((p) => p.id === activeId);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  return (
    <div className="project-menu" ref={ref}>
      <Button
        variant="ghost"
        size="sm"
        disabled={busy}
        onClick={() => setOpen((o) => !o)}
        title={t("project_menu_tip")}
      >
        {active?.name ?? t("default_project_name")} <ChevronDown size={13} />
      </Button>
      {open && (
        <div className="project-menu-pop">
          {projects.map((p) => (
            <button
              key={p.id}
              className="project-menu-item"
              onClick={() => {
                setOpen(false);
                onSwitch(p.id);
              }}
            >
              {p.id === activeId ? <Check size={12} /> : <span className="project-menu-gap" />}
              <span className="project-menu-name">{p.name}</span>
            </button>
          ))}
          <div className="project-menu-sep" />
          <button
            className="project-menu-item"
            onClick={() => {
              setOpen(false);
              onRename(activeId);
            }}
          >
            <Pencil size={12} /> <span className="project-menu-name">{t("rename")}</span>
          </button>
          <button
            className="project-menu-item project-menu-danger"
            onClick={() => {
              setOpen(false);
              onDelete(activeId);
            }}
          >
            <Trash2 size={12} /> <span className="project-menu-name">{t("delete")}</span>
          </button>
        </div>
      )}
    </div>
  );
}
