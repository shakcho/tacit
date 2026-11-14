import type { ReactNode } from "react";

import { AppShell } from "@/components/app-shell";

type ProductLayoutProps = {
  children: ReactNode;
};

export default function ProductLayout({ children }: ProductLayoutProps) {
  return <AppShell>{children}</AppShell>;
}
