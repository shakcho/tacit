import { redirect } from "next/navigation";

export default function LegacyVerifiedEmailPage(): never {
  redirect("/auth/sign-in");
}
