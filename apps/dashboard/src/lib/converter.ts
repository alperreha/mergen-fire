import type { CreateVMRequest } from "~/lib/mergen-api";

export type ConvertedImageArtifacts = {
  rootfs: boolean;
  agentDisk: boolean;
  payloadDisk: boolean;
  envDisk: boolean;
  suggestedVM: boolean;
};

export type ConvertedImagePaths = {
  rootfs: string;
  agentDisk: string;
  payloadDisk: string;
  envDisk: string;
  suggestedVM: string;
};

export type ConvertedImage = {
  id: string;
  image: string;
  outputDir: string;
  updatedAt: string;
  ready: boolean;
  artifacts: ConvertedImageArtifacts;
  paths: ConvertedImagePaths;
  suggestedRequest?: Partial<CreateVMRequest> | null;
};

export type ActiveConversion = {
  image: string;
  startedAt: string;
};

export type ConverterImagesResponse = {
  baseDir: string;
  total: number;
  items: ConvertedImage[];
  activeConversion: ActiveConversion | null;
};

export type ConverterConvertResponse = {
  status: "completed";
  conversion: {
    image: string;
    outputDir: string;
    stdout: string;
    stderr: string;
  };
  images: ConverterImagesResponse;
};
