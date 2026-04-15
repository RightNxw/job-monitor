import Link from "next/link";
import Image from "next/image";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="min-h-screen bg-[#080810] text-white">
      {/* Nav */}
      <nav className="border-b border-white/[0.06] px-6 py-4 flex items-center justify-between">
        <Link href="/" className="flex items-center gap-3">
          <Image
            src="/logo.png"
            alt="VSAT"
            width={676}
            height={369}
            priority
            className="h-7 w-auto invert"
          />
        </Link>
        <div className="flex items-center gap-6 text-sm text-white/50">
          <Link href="/dashboard" className="text-white/90 hover:text-white transition-colors">
            Jobs
          </Link>
          <Link href="/dashboard/pipeline" className="hover:text-white transition-colors">
            Pipeline
          </Link>
          <Link href="/dashboard/interviews" className="hover:text-white transition-colors">
            Interviews
          </Link>
          <Link href="/dashboard/signals" className="hover:text-white transition-colors">
            Signals
          </Link>
          <Link href="/dashboard/stories" className="hover:text-white transition-colors">
            Stories
          </Link>
        </div>
      </nav>
      <main className="max-w-7xl mx-auto px-6 py-8">{children}</main>
    </div>
  );
}
