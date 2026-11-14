import { redirect } from "next/navigation";

export default function LegacyAppLoginPage(): never {
  redirect("/auth/callback");
}
