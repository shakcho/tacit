import type { ReactNode } from "react";

type PageHeaderProps = {
  action?: ReactNode;
  description: string;
  eyebrow?: string;
  title: string;
};

export function PageHeader({ action, description, eyebrow, title }: PageHeaderProps) {
  return (
    <header className="flex flex-col gap-6 border-b border-border pb-8 sm:flex-row sm:items-end sm:justify-between">
      <div className="max-w-2xl">
        {eyebrow ? <p className="mb-3 text-sm font-medium text-muted-foreground">{eyebrow}</p> : null}
        <h1 className="text-3xl font-medium tracking-[-0.035em] text-foreground sm:text-4xl">{title}</h1>
        <p className="mt-3 max-w-xl text-base leading-7 text-muted-foreground">{description}</p>
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </header>
  );
}
