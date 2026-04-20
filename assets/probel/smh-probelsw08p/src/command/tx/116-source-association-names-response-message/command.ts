import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { MetaCommandBase } from '../../meta-command.base';
import { CommandOptionsUtility, SourceAssociationNamesResponseCommandOptions } from './options';
import { CommandParamsUtility, SourceAssociationNamesResponseCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Source Names Response Message command
 *
 * This command is issued by the controller in response to an ALL SOURCE ASSOCIATION NAMES REQUEST or a SINGLE SOURCE ASSOCIATION NAME REQUEST message (Command Bytes 114 and 115).
 * The message length restrictions in 3.2.19 apply.
 *
 * Command issued by Pro-Bel Controller
 * @export
 * @class SourceAssociationNamesResponseCommand
 * @extends {MetaCommandBase<SourceAssociationNamesResponseCommandParams>}
 */
export class SourceAssociationNamesResponseCommand extends MetaCommandBase<
    SourceAssociationNamesResponseCommandParams,
    SourceAssociationNamesResponseCommandOptions
> {
    /**
     * Creates an instance of SourceAssociationNamesResponseCommand.
     * @param {SourceAssociationNamesResponseCommandParams} params the command parameters
     * @param {SourceAssociationNamesResponseCommandOptions} _options the command options
     * @memberof SourceAssociationNamesResponseCommand
     */
    constructor(
        params: SourceAssociationNamesResponseCommandParams,
        options: SourceAssociationNamesResponseCommandOptions
    ) {
        super(CommandIdentifiers.TX.GENERAL.SOURCE_ASSOCIATION_NAMES_RESPONSE_MESSAGE, params, options);
        this.validateParams(params, options);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof SourceAssociationNamesResponseCommand
     */
    toLogDescription(): string {
        return `General  -   ${this.name}: ${CommandParamsUtility.toString(
            this.params
        )}, ${CommandOptionsUtility.toString(this.options)}`;
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof SourceAssociationNamesResponseCommand
     */
    protected buildData(): Buffer[] {
        // Padding all the sourceNameItems with Length of Names Required
        const sourceNameAssociationItemsPadding = new Array<string>();
        for (let nbrPadding = 0; nbrPadding < this.params.sourceAssociationNameItems.length; nbrPadding++) {
            sourceNameAssociationItemsPadding.push(
                BufferUtility.decPadStringEnd(
                    this.params.sourceAssociationNameItems[nbrPadding],
                    this.options.lengthOfNames.byteLength,
                    // TODO: Add in the admin settings and could be changed by the admin at anytime.
                    '-'
                )
            );
        }

        // Creates an array of sourceNameItemsPadding split into groups the length of the byteMaximumNumberOfNames.
        const sourceNameItemsChunck = BufferUtility.getChunkOfStringArray(
            sourceNameAssociationItemsPadding,
            this.options.lengthOfNames.byteMaximumNumberOfNames
        );
        // Get the first time the First name number
        let firstSourceNumberOffsetId = this.params.firstSourceId;

        // Get the list of command to send
        const buildDataArray = new Array<Buffer>();

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

        return buildDataArray;
    }

    /**
     * Builds the normal command
     * Returns Probel SW-P-08 - General Source Association Names Response Message CMD_116_0X74
     *
     * + This command is issued by the controller in response to an ALL SOURCE ASSOCIATION NAMES REQUEST or a SINGLE SOURCE ASSOCIATION NAME REQUEST message (Command Bytes 114 and 115).
     * + The message length restrictions in 3.2.19 apply.
     *
     * + Note: Byte 5 will always be set to 01 in response to command 115. Also in this message, a maximum of 32 4-char names, 16 8-char names or 10 12-char names.
     *
     * | Message                        | Command Byte | 116 - 0x74                                                                                                   |
     * |--------------------------------|--------------|--------------------------------------------------------------------------------------------------------------|
     * | Byte                           | Field Format | Notes                                                                                                        |
     * | Byte 1                         | Matrix/Level | Matrix/Level number as defined in 3.1.2                                                                      |
     * | Byte 2                         | Names length | Length of Source Association Names Returned as defined in 3.1.18                                             |
     * | Byte 3                         | 1st Src mult | First Source Association number multiplier DIV 256                                                           |
     * | Byte 4                         | 1st Src num  | First Source number MOD 256                                                                                  |
     * | Byte 5                         | Num of names | Number of Source Names to follow                                                                             |
     * | If Byte 2 = 00 (4-Char names)  |              |                                                                                                              |
     * | Bytes 6-9                      | 1st Name     | First 4-char Source Association name                                                                         |
     * | Bytes 10-13                    | 2nd Name     | Second 4-char Source Association name                                                                        |
     * | Bytes 14-17                    | 3rd Name     | Third 4-char Source Association name                                                                         |
     * | Etc ...                        |              |                                                                                                              |
     * | If Byte 2 = 01 (8-Char names)  |              |                                                                                                              |
     * | Bytes 6-13                     | 1st Name     | First 8-char Source Association name                                                                         |
     * | Bytes 14-21                    | 2nd Name     | Second 8-char Source Association name                                                                        |
     * | Bytes 22-30                    | 3rd Name     | Third 8-char Source Association name                                                                         |
     * | Etc ...                        |              |                                                                                                              |
     * | If Byte 2 = 02 (12-Char names) |              |                                                                                                              |
     * | Bytes 6-17                     | 1st Name     | First 12-char Source Association name                                                                        |
     * | Bytes 18-29                    | 2nd Name     | Second 12-char Source Association name                                                                       |
     * | Etc ...                        |              |                                                                                                              |
     *
     * @private
     * @returns {Buffer}
     * @memberof SourceAssociationNamesResponseCommand
     */
    protected buildDataNormal(
        params: SourceAssociationNamesResponseCommandParams,
        options: SourceAssociationNamesResponseCommandOptions,
        items: string[],
        sourceOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            // (8 bytes of the command) + (Length of the names selected by the user to multiple by the number of source names to send)
            this.options.lengthOfNames.byteLength * items.length + 6;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })
            .writeUInt8(CommandIdentifiers.TX.GENERAL.SOURCE_NAMES_RESPONSE_MESSAGE.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(params.matrixId, params.levelId)) // Byte 1 : Matrix Number (0 – 15)
            .writeUInt8(options.lengthOfNames.type) // Byte 2 : Length of Names Required
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
     * Validate the parameters, options and items and throw a ValidationError in case of error
     * @private
     * @param {SourceAssociationNamesResponseCommandParams} params command parameters
     * @param {DestinationAssociationNamesResponseCommandOptions} options the command items
     * @memberof CrossPointTieLineTallyCommand
     */
    private validateParams(
        params: SourceAssociationNamesResponseCommandParams,
        options: SourceAssociationNamesResponseCommandOptions
    ): void {
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
