import LoginForm from "@/components/LoginForm";

export const metadata = { title: "Sign in · optionsBot monitor" };

export default function LoginPage() {
  return (
    <main className="flex min-h-screen items-center justify-center p-4">
      <LoginForm />
    </main>
  );
}
