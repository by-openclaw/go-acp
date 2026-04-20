import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { MetaCommandBase } from '../../meta-command.base';
import { CommandOptionsUtility, DestinationAssociationNamesResponseCommandOptions } from './options';
import { CommandParamsUtility, DestinationAssociationNamesResponseCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Destination Association Names Response Message command
 *
 * + This command is issued by the controller in response to an ALL DESTINATION ASSOCIATION NAMES REQUEST or SINGLE DESTINATION ASSOCIATION NAME REQUEST messages (Command Bytes 102 and 103).
 * + The message length restrictions in 3.2.19 apply.
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class DestinationAssociationNamesResponseCommand
 * @extends {MetaCommandBase<DestinationAssociationNamesResponseCommandParams>}
 */
export class DestinationAssociationNamesResponseCommand extends MetaCommandBase<
    DestinationAssociationNamesResponseCommandParams,
    DestinationAssociationNamesResponseCommandOptions
> {
    /**
     * Creates an instance of DestinationAssociationNamesResponseCommand.
     * @param {DestinationAssociationNamesResponseCommandParams} params the command parameters
     * @param {DestinationAssociationNamesResponseCommandOptions} _options the command options
     * @memberof DestinationAssociationNamesResponseCommand
     */
    constructor(
        params: DestinationAssociationNamesResponseCommandParams,
        options: DestinationAssociationNamesResponseCommandOptions
    ) {
        super(CommandIdentifiers.TX.GENERAL.DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE, params, options);
        this.validateParams(params, options);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual description
     * @memberof DestinationAssociationNamesResponseCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
                this.options
            )}`;

        return descriptionFor('General');
    }

    /**
     * Build the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof DestinationAssociationNamesResponseCommand
     */
    protected buildData(): Buffer[] {
        // Padding all the DestinationAssociationNames with Length of Names Required
        const DestinationAssociationNamesPadding = new Array<string>();
        for (let nbrPadding = 0; nbrPadding < this.params.destinationAssociationNameItems.length; nbrPadding++) {
            DestinationAssociationNamesPadding.push(
                BufferUtility.decPadStringEnd(
                    this.params.destinationAssociationNameItems[nbrPadding],
                    this.options.lengthOfDestinationAssociatonNamesReturned.byteLength,
                    // TODO: Add in the admin settings and could be changed by the admin at anytime.
                    '-'
                )
            );
        }

        // Creates an array of DestinationAssociationNamesPadding split into groups the length of the byteMaximumNumberOfNames.
        const DestinationAssociationNamesChunck = BufferUtility.getChunkOfStringArray(
            DestinationAssociationNamesPadding,
            this.options.lengthOfDestinationAssociatonNamesReturned.byteMaximumNumberOfNames
        );
        // Get the first time the First name number
        let firstSourceNumberOffsetId = this.params.firstDestinationAssociationId;

        // Get the list of command to send
        const buildDataArray = new Array<Buffer>();

        if (this.params.matrixId > 15 || this.params.levelId > 15) {
            // Generate the List of Extended Command to send
            for (let nbrCmdToSend = 0; nbrCmdToSend < DestinationAssociationNamesChunck.length; nbrCmdToSend++) {
                buildDataArray.push(
                    this.buildDataExtended(
                        this.params,
                        this.options,
                        DestinationAssociationNamesChunck[nbrCmdToSend],
                        firstSourceNumberOffsetId
                    )
                );
                // Add the new First Name Number update for next command
                firstSourceNumberOffsetId += (nbrCmdToSend + 1) * DestinationAssociationNamesChunck[nbrCmdToSend].length;
            }
        } else {
            // Generate the List of General Commands to send
            for (let nbrCmdToSend = 0; nbrCmdToSend < DestinationAssociationNamesChunck.length; nbrCmdToSend++) {
                buildDataArray.push(
                    this.buildDataNormal(
                        this.params,
                        this.options,
                        DestinationAssociationNamesChunck[nbrCmdToSend],
                        firstSourceNumberOffsetId
                    )
                );

                // Add the new First Name Number update for next command
                firstSourceNumberOffsetId += (nbrCmdToSend + 1) * DestinationAssociationNamesChunck[nbrCmdToSend].length;
            }
        }
        return buildDataArray;
    }

    /**
     * Builds the general command
     * Returns Probel SW-P-08 - General Destination Association Names Response Message CMD_107_0X6b
     *
     * + This command is issued by the controller in response to an ALL DESTINATION ASSOCIATION NAMES REQUEST or SINGLE DESTINATION ASSOCIATION NAME REQUEST messages (Command Bytes 102 and 103).
     * + The message length restrictions in 3.2.19 apply.
     *
     * | Message                        | Command Byte | 107_0X6b                                                                                                     |
     * |--------------------------------|--------------|--------------------------------------------------------------------------------------------------------------|
     * | Byte                           | Field Format | Notes                                                                                                        |
     * | Byte 1                         | Matrix/Level | Matrix/Level number as defined in 3.1.2                                                                      |
     * | Byte 2                         | Names length | Length of Destination Association Names Returned as defined in 3.1.18                                        |
     * | Byte 3                         | 1st Dest mult| First Destination Associationnumber multiplier DIV 256                                                       |
     * | Byte 4                         | 1st Dest num | First Destination Associationnumber MOD 256                                                                  |
     * | Byte 5                         | Num of names | Number of Destination AssociationNames to follow                                                             |
     * | If Byte 2 = 00 (4-Char names)  |              |                                                                                                              |
     * | Bytes 6-9                      | 1st Name     | First 4-char Destination Associationname                                                                     |
     * | Bytes 10-13                    | 2nd Name     | Second 4-char Destination Associationname                                                                    |
     * | Bytes 14-17                    | 3rd Name     | Third 4-char Destination Associationname                                                                     |
     * | Etc ...                        |              |                                                                                                              |
     * | If Byte 2 = 01 (8-Char names)  |              |                                                                                                              |
     * | Bytes 6-13                     | 1st Name     | First 8-char Destination Associationname                                                                     |
     * | Bytes 14-21                    | 2nd Name     | Second 8-char Destination Associationname                                                                    |
     * | Bytes 22-30                    | 3rd Name     | Third 8-char Destination Associationname                                                                     |
     * | Etc ...                        |              |                                                                                                              |
     * | If Byte 2 = 02 (12-Char names) |              |                                                                                                              |
     * | Bytes 6-17                     | 1st Name     | First 12-char Destination Associationname                                                                    |
     * | Bytes 18-29                    | 2nd Name     | Second 12-char Destination Associationname                                                                   |
     * | Etc ...                        |              |                                                                                                              |
     *
     * @private
     * @param {DestinationAssociationNamesResponseCommandParams} params
     * @param {DestinationAssociationNamesResponseCommandOptions} options
     * @param {string[]} items
     * @param {number} sourceOffsetId
     * @returns {Buffer} the command message
     * @memberof DestinationAssociationNamesResponseCommand
     */
    private buildDataNormal(
        params: DestinationAssociationNamesResponseCommandParams,
        options: DestinationAssociationNamesResponseCommandOptions,
        items: string[],
        sourceOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            // (8 bytes of the command) + (Length of the names selected by the user to multiple by the number of source names to send)
            this.options.lengthOfDestinationAssociatonNamesReturned.byteLength * items.length + 6;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })
            .writeUInt8(CommandIdentifiers.TX.GENERAL.DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(params.matrixId, params.levelId)) // Byte 1 : Matrix Number (0 – 15)
            .writeUInt8(options.lengthOfDestinationAssociatonNamesReturned.type) // Byte 2 : Length of Names Required
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
     * Returns Probel SW-P-08 - Extended General General All Source Names Request Message CMD_235_0Xeb
     *
     * + This command is issued by the controller in response to an EXTENDED ALL DESTINATION ASSOCIATION NAMES REQUEST or EXTENDED SINGLE DESTINATION ASSOCIATION NAME REQUEST messages (Command Bytes 230 and 231).
     * + The message length restrictions in 3.5.10 apply.
     *
     * + Note: Byte 6 will always be set to 01 in response to command 231
     *
     * | Message        | Command Byte  | 235 - 0xeb                                                                                                                        |
     * |----------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
     * | Byte           | Field Format  | Notes                                                                                                                             |
     * | Byte 1         | Matrix number |                                                                                                                                   |
     * | Byte 2         | Level number  |                                                                                                                                   |
     * | Byte3          | Length of nam | Length of Destination Association Names Returned 3.4.10                                                                           |
     * | Byte 4         | 1st Src mult  | First Destination Association number multiplier DIV 256                                                                           |
     * | Byte 5         | 1st Src num   | First Destination Association number MOD 256                                                                                      |
     * | Byte 6         | Num of names  | Number of Destination Association Names to follow (in this message, maximum of 32 for 4 char names, 16 for 8 char names and 10 for 12 char names). |
     * | If Byte 3 = 00 |               |                                                                                                                                   |
     * | Byte 7-10      | 1st Name      | 1st 4-char Destination Association name                                                                                           |
     * | Byte 11-14     | 2nd Name      | 2nd 4-char Destination Association name                                                                                           |
     * | Byte 15-18     | 3rd Name      | 3rd 4-char Destination Association name                                                                                           |
     * | Etc ...        |               |                                                                                                                                   |
     * | If Byte 3 = 01 |               |                                                                                                                                   |
     * | Byte 7-14      | 1st Name      | 1st 8-char Destination Association name                                                                                           |
     * | Byte 15-22     | 2nd Name      | 2nd 8-char Destination Association name                                                                                           |
     * | Etc ...        |               |                                                                                                                                   |
     * | If Byte 3 = 02 |               |                                                                                                                                   |
     * | Byte 7-18      | 1st Name      | 1st 12-char Destination Association name                                                                                          |
     * | Byte 19-30     | 2nd Name      | 2nd 12-char Destination Association name                                                                                          |
     * | Etc ...        |               |                                                                                                                                   |
     *
     * @private
     * @param {DestinationAssociationNamesResponseCommandParams} params
     * @param {DestinationAssociationNamesResponseCommandOptions} options
     * @param {string[]} items
     * @param {number} sourceOffsetId
     * @returns {Buffer} the command message
     * @memberof DestinationAssociationNamesResponseCommand
     */
    private buildDataExtended(
        params: DestinationAssociationNamesResponseCommandParams,
        options: DestinationAssociationNamesResponseCommandOptions,
        items: string[],
        sourceOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            // (8 bytes of the command) + (Length of the names selected by the user to multiple by the number of source names to send)
            this.options.lengthOfDestinationAssociatonNamesReturned.byteLength * items.length + 7;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })
            .writeUInt8(CommandIdentifiers.TX.EXTENDED.DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE.id)
            .writeUInt8(params.matrixId) // Byte 1 : Matrix Number (0 – 255)
            .writeUInt8(params.levelId) // Byte 2 : Matrix Number (0 – 255)
            .writeUInt8(options.lengthOfDestinationAssociatonNamesReturned.type) // Byte 3 : Length of Names Required
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
     * @private
     * @param {DestinationAssociationNamesResponseCommandParams} params the command parameters
     * @param {DestinationAssociationNamesResponseCommandOptions} options the command options
     * @memberof DestinationAssociationNamesResponseCommand
     */
    private validateParams(
        params: DestinationAssociationNamesResponseCommandParams,
        options: DestinationAssociationNamesResponseCommandOptions
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
