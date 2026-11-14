import { redirect } from "next/navigation";

type LegacyMeetingUploadPageProps = {
  params: Promise<{ meetingId: string }>;
};

export default async function LegacyMeetingUploadPage({ params }: LegacyMeetingUploadPageProps): Promise<never> {
  const { meetingId } = await params;
  redirect(`/meetings/${encodeURIComponent(meetingId)}/upload`);
}
