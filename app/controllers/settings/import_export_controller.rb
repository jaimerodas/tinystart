# Settings → Import/Export: a user's start page out as the YAML interchange
# format in docs/start-page-format.md, and back in again. The format and the
# reasons behind it are in that file; the work is in StartPageExporter and
# StartPageImporter.
class Settings::ImportExportController < ApplicationController
  # A start page is a few dozen tiles. Anything this side of it is either a
  # mistake or a file that has no business being read into memory.
  MAX_BYTES = 512.kilobytes

  def show
  end

  def export
    send_data StartPageExporter.new(current_user).call,
              filename: "tinystart-start-page-#{Date.current.iso8601}.yml",
              type: "text/yaml",
              disposition: "attachment"
  end

  def create
    source = uploaded_source
    return if performed?

    importer = StartPageImporter.new(current_user, source)

    if importer.call
      # The warning rides along with the success: the import happened, and the
      # counts in the file's header did not describe what arrived.
      redirect_to settings_import_export_path,
                  notice: [ imported(importer.summary), importer.warning ].compact.join(" ")
    else
      redirect_to settings_import_export_path, alert: "Nothing was imported: #{importer.error}"
    end
  end

  private

  # Returns the file's contents, or redirects and returns nil.
  def uploaded_source
    file = params[:file]

    return refuse("choose a file to import first.") if file.blank? || !file.respond_to?(:read)
    return refuse("that file is too large to be a start page.") if file.size > MAX_BYTES

    # The real data is in Spanish, and the tempfile is opened in binary mode, so
    # a file that isn't UTF-8 would import mangled names rather than fail.
    source = file.read.to_s.force_encoding(Encoding::UTF_8)
    return refuse("that file isn't valid UTF-8 text.") unless source.valid_encoding?

    source
  end

  def refuse(message)
    redirect_to settings_import_export_path, alert: message.upcase_first
    nil
  end

  def imported(summary)
    "Imported #{summary[:items]} #{'link'.pluralize(summary[:items])} in " \
    "#{summary[:groups]} #{'group'.pluralize(summary[:groups])} across " \
    "#{summary[:columns]} #{'column'.pluralize(summary[:columns])}."
  end
end
