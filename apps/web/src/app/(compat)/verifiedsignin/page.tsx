import { redirect } from "next/navigation";

export default function LegacyVerifiedSignInPage(): never {
  redirect("/auth/sign-in");
}
