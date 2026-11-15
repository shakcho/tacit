import { redirect } from "next/navigation";

export default function LegacyVerifiedSignUpPage(): never {
  redirect("/auth/sign-in");
}
