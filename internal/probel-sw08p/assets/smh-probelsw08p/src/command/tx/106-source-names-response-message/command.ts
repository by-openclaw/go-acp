import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { MetaCommandBase } from '../../meta-command.base';
import { CommandOptionsUtility, SourceNamesResponseCommandOptions } from './options';
import { CommandParamsUtility, SourceNamesResponseCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Source Names Response Message command
 *
 * + This command is issued by the controller in response to ALL SOURCE NAMES REQUEST and SINGLE SOURCE NAME REQUEST messages (Command Bytes 100 and 101).
 * + The message length is limited to 134 bytes, thus name tables that would exceed this length would cause more than one message to be issued with the appropriate source number value in the third field of the message.
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class SourceNamesResponseCommand
 * @extends {MetaCommandBase<SourceNamesResponseCommandParams>}
 */
export class SourceNamesResponseCommand extends MetaCommandBase<
    SourceNamesResponseCommandParams,
    SourceNamesResponseCommandOptions
> {
    /**
     * Creates an instance of SourceNamesResponseCommand.
     * @param {SourceNamesResponseCommandParams} params the command parameters
     * @param {SourceNamesResponseCommandOptions} _options the command options
     * @memberof SourceNamesResponseCommand
     */
    constructor(params: SourceNamesResponseCommandParams, options: SourceNamesResponseCommandOptions) {
        super(CommandIdentifiers.TX.GENERAL.SOURCE_NAMES_RESPONSE_MESSAGE, params, options);
        this.validateParams(params, options);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual description
     * @memberof SourceNamesResponseCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
                this.options
            )}`;

        return descriptionFor('General');
    }

    /**
     * Build the Pro-Bel SW-P-8 - Source Names Response Message
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof SourceNamesResponseCommand
     */
    protected buildData(): Buffer[] {
        // Padding all the sourceNameItems with Length of Names Required
        const sourceNameItemsPadding = new Array<string>();
        for (let nbrPadding = 0; nbrPadding < this.params.sourceNameItems.length; nbrPadding++) {
            sourceNameItemsPadding.push(
                BufferUtility.decPadStringEnd(
                    this.params.sourceNameItems[nbrPadding],
                    this.options.lengthOfSourceNamesReturned.byteLength,
                    // TODO: Add in the admin settings and could be changed by the admin at anytime.
                    '-'
                )
            );
        }

        // Creates an array of sourceNameItemsPadding split into groups the length of the byteMaximumNumberOfNames.
        const sourceNameItemsChunck = BufferUtility.getChunkOfStringArray(
            sourceNameItemsPadding,
            this.options.lengthOfSourceNamesReturned.byteMaximumNumberOfNames
        );
        // Get the first time the First name number
        let firstSourceNumberOffsetId = this.params.firstSourceId;

        // Get the list of command to send
        const buildDataArray = new Array<Buffer>();

        if (this.params.matrixId > 15 || this.params.levelId > 15) {
            // Generate the List of Extended Command to send
            for (let nbrCmdToSend = 0; nbrCmdToSend < sourceNameItemsChunck.length; nbrCmdToSend++) {
                buildDataArray.push(
                    this.buildDataExtended(
                        this.params,
                        this.options,
                        sourceNameItemsChunck[nbrCmdToSend],
                        firstSourceNumberOffsetId
                    )
                );
                // Add the new First Name Number update for next command
                firstSourceNumberOffsetId += (nbrCmdToSend + 1) * sourceNameItemsChunck[nbrCmdToSend].length;
            }
        } else {
            // Generate the List of General Commands to send
            for (let nbrCmdToSend = 0; nbrCmdToSend < sourceNameItemsChunck.length; nbrCmdToSend++) {
                buildDataArray.push(
                    this.buildDataNormal(
                        this.params,
                        this.options,
                        sourceNameItemsChunck[nbrCmdToSend],
                        firstSourceNumberOffsetId
                    )
                );

                // Add the new First Name Number update for next command
                firstSourceNumberOffsetId += (nbrCmdToSend + 1) * sourceNameItemsChunck[nbrCmdToSend].length;
            }
        }
        return buildDataArray;
    }

    /**
     * Builds the general command
     * Returns Probel SW-P-08 - General Source Names Response Message CMD_106_0X6a
     *
     * + This command is issued by the controller in response to ALL SOURCE NAMES REQUEST and SINGLE SOURCE NAME REQUEST messages (Command Bytes 100 and 101).
     * + The message length is limited to 134 bytes, thus name tables that would exceed this length would cause more than one message to be issued with the appropriate source number value in the third field of the message.
     * + Note: Byte 5  will always be set to 01 in response to command 101. Also in this message, a maximum of 32 4-char names, 16 8-char names or 10 12-char names.
     *
     * | Message                        | Command Byte | 106_0X6a                                                                                                     |
     * |--------------------------------|--------------|--------------------------------------------------------------------------------------------------------------|
     * | Byte                           | Field Format | Notes                                                                                                        |
     * | Byte 1                         | Matrix/Level | Matrix/Level number as defined in 3.1.2                                                                      |
     * | Byte 2                         | Names length | Length of Source Names Returned as defined in 3.1.18                                                         |
     * | Byte 3                         | 1st Src mult | First Source number multiplier DIV 256                                                                       |
     * | Byte 4                         | 1st Src num  | First Source number MOD 256                                                                                  |
     * | Byte 5                         | Num of names | Number of Source Names to follow                                                                             |
     * | If Byte 2 = 00 (4-Char names)  |              |                                                                                                              |
     * | Bytes 6-9                      | 1st Name     | First 4-char Source name                                                                                     |
     * | Bytes 10-13                    | 2nd Name     | Second 4-char Source name                                                                                    |
     * | Bytes 14-17                    | 3rd Name     | Third 4-char Source name                                                                                     |
     * | Etc ...                        |              |                                                                                                              |
     * | If Byte 2 = 01 (8-Char names)  |              |                                                                                                              |
     * | Bytes 6-13                     | 1st Name     | First 8-char Source name                                                                                     |
     * | Bytes 14-21                    | 2nd Name     | Second 8-char Source name                                                                                    |
     * | Bytes 22-30                    | 3rd Name     | Third 8-char Source name                                                                                     |
     * | Etc ...                        |              |                                                                                                              |
     * | If Byte 2 = 02 (12-Char names) |              |                                                                                                              |
     * | Bytes 6-17                     | 1st Name     | First 12-char Source name                                                                                    |
     * | Bytes 18-29                    | 2nd Name     | Second 12-char Source name                                                                                   |
     * | Etc ...                        |              |                                                                                                              |
     *
     * @private
     * @param {SourceNamesResponseCommandParams} params
     * @param {SourceNamesResponseCommandOptions} options
     * @param {string[]} items
     * @param {number} sourceOffsetId
     * @returns {Buffer} the command message
     * @memberof SourceNamesResponseCommand
     */
    private buildDataNormal(
        params: SourceNamesResponseCommandParams,
        options: SourceNamesResponseCommandOptions,
        items: string[],
        sourceOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            // (8 bytes of the command) + (Length of the names selected by the user to multiple by the number of source names to send)
            this.options.lengthOfSourceNamesReturned.byteLength * items.length + 6;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })
            .writeUInt8(CommandIdentifiers.TX.GENERAL.SOURCE_NAMES_RESPONSE_MESSAGE.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(params.matrixId, params.levelId)) // Byte 1 : Matrix Number (0 – 15)
            .writeUInt8(options.lengthOfSourceNamesReturned.type) // Byte 2 : Length of Names Required
            .writeUInt8(Math.floor(sourceOffsetId / 256)) // Byte 3 : First name number multiplier DIV 256
            .writeUInt8(sourceOffsetId % 256) // Byte 4 : First name number MOD 256
            .writeUInt8(items.length); // Byte 5 : Number of names to follow ...
        for (let itemIndex = 0; itemIndex < items.length; itemIndex++) {
            buffer.writeString(items[itemIndex]);
        }
        // Add the command buffer to the array
        return buffer.toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - Extended General General All Source Names Request Message CMD_234_0Xea
     *
     * This message is issued by the remote device to request the names for all the sources on a given matrix and level.
     * The controller will respond with one or more EXTENDED SOURCE NAME RESPONSE messages (Command Byte 234).
     *
     * Note: Byte 6 will always be set to 01 in response to command 229.
     *
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | Message        | Command Byte  | 234_0Xea                                                                                                                          |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | Byte           | Field Format  | Notes                                                                                                                             |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | Byte 1         | Matrix number |                                                                                                                                   |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | Byte 2         | Level number  |                                                                                                                                   |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | Byte3          | Length of nam | Length of Source Names Returned 3.4.10                                                                                            |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | Byte 4         | 1st Src mult  | First Source number multiplier DIV 256                                                                                            |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | Byte 5         | 1st Src num   | First Source number MOD 256                                                                                                       |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | Byte 6         | Num of names  | Number of Source Names to follow (in this message, maximum of 32 for 4 char names, 16 for 8 char names and 10 for 12 char names). |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | If Byte 3 = 00 |               |                                                                                                                                   |
     | Byte 7-10      | 1st Name      | 1st 4-char Source name                                                                                                            |
     | Byte 11-14     | 2nd Name      | 2nd 4-char Source name                                                                                                            |
     | Byte 15-18     | 3rd Name      | 3rd 4-char Source name                                                                                                            |
     | Etc ...        |               |                                                                                                                                   |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | If Byte 3 = 01 |               |                                                                                                                                   |
     | Byte 7-14      | 1st Name      | 1st 8-char Source name                                                                                                            |
     | Byte 15-22     | 2nd Name      | 2nd 8-char Source name                                                                                                            |
     | Etc ...        |               |                                                                                                                                   |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     | If Byte 3 = 02 |               |                                                                                                                                   |
     | Byte 7-18      | 1st Name      | 1st 12-char Source name                                                                                                           |
     | Byte 19-30     | 2nd Name      | 2nd 12-char Source name                                                                                                           |
     | Etc ...        |               |                                                                                                                                   |
     |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     *
     * @private
     * @param {SourceNamesResponseCommandParams} params
     * @param {SourceNamesResponseCommandOptions} options
     * @param {string[]} items
     * @param {number} sourceOffsetId
     * @returns {Buffer} the command message
     * @memberof SourceNamesResponseCommand
     */
    private buildDataExtended(
        params: SourceNamesResponseCommandParams,
        options: SourceNamesResponseCommandOptions,
        items: string[],
        sourceOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            // (8 bytes of the command) + (Length of the names selected by the user to multiple by the number of source names to send)
            this.options.lengthOfSourceNamesReturned.byteLength * items.length + 7;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })
            .writeUInt8(CommandIdentifiers.TX.EXTENDED.SOURCE_NAMES_RESPONSE_MESSAGE.id)
            .writeUInt8(params.matrixId) // Byte 1 : Matrix Number (0 – 255)
            .writeUInt8(params.levelId) // Byte 2 : Matrix Number (0 – 255)
            .writeUInt8(options.lengthOfSourceNamesReturned.type) // Byte 3 : Length of Names Required
            .writeUInt8(Math.floor(sourceOffsetId / 256)) // Byte 4 : First name number multiplier DIV 256
            .writeUInt8(sourceOffsetId % 256) // Byte 5 : First name number MOD 256
            .writeUInt8(items.length); // Byte 6 : Number of names to follow ... Max = 64
        for (let itemIndex = 0; itemIndex < items.length; itemIndex++) {
            buffer.writeString(items[itemIndex]);
        }
        // Add the command buffer to the array
        return buffer.toBuffer();
    }

    /**
     * Validates the command parameters, options, items and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {UpdateNameRequestCommandParams} params the command parameters
     * @param {UpdateRenameRequestCommandOptions} options the command options
     * @param {UpdateRenameRequestCommandItems} items the command itesm
     * @memberof UpdateRenameRequestCommand
     */
    private validateParams(params: SourceNamesResponseCommandParams, options: SourceNamesResponseCommandOptions): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params, options).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }
}
