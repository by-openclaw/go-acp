import _ from 'lodash';
import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { MetaCommandBase } from '../../meta-command.base';
import { CommandItemsUtility, ProtectTallyDumpCommandItems } from './items';
import { CommandOptionsUtility } from './options';
import { CommandParamsUtility, ProtectTallyDumpCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Protect Tally Dump Message command
 *
 * + This message is issued by the Pro-Bel Controller in response to a PROTECT DUMP REQUEST (Command Byte 19).
 * + It returns all the Protect Information.
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class ProtectTallyDumpCommand
 * @extends {MetaCommandBase<ProtectTallyDumpCommandParams>}
 */
export class ProtectTallyDumpCommand extends MetaCommandBase<ProtectTallyDumpCommandParams, any> {
    /**
     * Creates an instance of ProtectTallyDumpCommand.
     * @param {ProtectTallyDumpCommandParams} params the command parameters
     * @memberof ProtectTallyDumpCommand
     */
    constructor(params: ProtectTallyDumpCommandParams) {
        super(CommandIdentifiers.TX.GENERAL.PROTECT_TALLY_DUMP_MESSAGE, params);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof ProtectTallyDumpCommand
     */
    // TODO: Add deviceNumberProtectDataItems{Device,ProtectData} array
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
                this.options
            )}, ${CommandItemsUtility.toString(this.params.deviceNumberProtectDataItems[0])}`;

        return descriptionFor('General');
    }

    /**
     * Build the Pro-Bel SW-P-8 - Protect Tally Dump Message
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof ProtectTallyDumpCommand
     */
    protected buildData(): Buffer[] {
        // build Extended Command & Add the command buffer to the array
        if (this.params.matrixId > 15 || this.params.levelId > 15 || this.params.firstDestinationId > 895) {
            // Creates an array of deviceNumberProtectDataItems split into groups the length of the numberOfProtectTallies.
            const deviceNumberProtectDataItemsChunck = _.chunk(
                this.params.deviceNumberProtectDataItems,
                this.params.numberOfProtectTallies
            );
            // Get the first time the First Destination number
            let destinationOffsetId = this.params.firstDestinationId;
            // Get the list of commands to send
            const buildDataArray = new Array<Buffer>();

            // Generate the List of Extended Command to send
            for (let nbrCmdToSend = 0; nbrCmdToSend < deviceNumberProtectDataItemsChunck.length; nbrCmdToSend++) {
                // Add the command buffer to the array
                buildDataArray.push(
                    this.buildDataExtended(
                        this.params,
                        deviceNumberProtectDataItemsChunck[nbrCmdToSend],
                        destinationOffsetId
                    )
                );

                // Add the new First Name Number update for next command
                destinationOffsetId += deviceNumberProtectDataItemsChunck[nbrCmdToSend].length;
            }
            return buildDataArray;
        } else {
            // build General Command & Add the command buffer to the array
            if (this.params.firstDestinationId + this.params.deviceNumberProtectDataItems.length < 896) {
                // Creates an array of deviceNumberProtectDataItems split into groups the length of the numberOfProtectTallies.
                const deviceNumberProtectDataItemsChunck = _.chunk(
                    this.params.deviceNumberProtectDataItems,
                    this.params.numberOfProtectTallies
                );

                // Gets the first time the First destination number
                let destinationOffsetId = this.params.firstDestinationId;

                // Gets the list of general command to send
                const buildDataArray = new Array<Buffer>();

                // Generate the List of Commands to send
                for (let nbrCmdToSend = 0; nbrCmdToSend < deviceNumberProtectDataItemsChunck.length; nbrCmdToSend++) {
                    // Add the command buffer to the array
                    buildDataArray.push(
                        this.buildDataNormal(
                            this.params,
                            deviceNumberProtectDataItemsChunck[nbrCmdToSend],
                            destinationOffsetId
                        )
                    );

                    // Add the new First Destination Number update for next command
                    destinationOffsetId += deviceNumberProtectDataItemsChunck[nbrCmdToSend].length;
                }
                return buildDataArray;
            } else {
                // Creates an array of deviceNumberProtectDataItems split into groups the length of the (896 - firstDestinationId).
                const calculateMaxItemsisNormal = 896 - this.params.firstDestinationId;
                // general commands
                const deviceNumberProtectDataItemsNormalSliced = _.slice(
                    this.params.deviceNumberProtectDataItems,
                    0,
                    calculateMaxItemsisNormal
                );
                // Extended commands
                const deviceNumberProtectDataItemsExtendedSliced = _.slice(
                    this.params.deviceNumberProtectDataItems,
                    calculateMaxItemsisNormal,
                    this.params.deviceNumberProtectDataItems.length
                );
                // general commands
                // Creates an array of deviceNumberProtectDataItemsNormalChunck split into groups the length of the numberOfProtectTallies.
                const deviceNumberProtectDataItemsNormalChunck = _.chunk(
                    deviceNumberProtectDataItemsNormalSliced,
                    this.params.numberOfProtectTallies
                );

                // Gets the first time the First Destination number
                let destinationOffsetId = this.params.firstDestinationId;

                // Gets the list of general command to send
                const buildDataArray = new Array<Buffer>();

                // Generate the List of General Command to send
                for (
                    let nbrCmdToSend = 0;
                    nbrCmdToSend < deviceNumberProtectDataItemsNormalChunck.length;
                    nbrCmdToSend++
                ) {
                    // Add the general command buffer to the array
                    buildDataArray.push(
                        this.buildDataNormal(
                            this.params,
                            deviceNumberProtectDataItemsNormalChunck[nbrCmdToSend],
                            destinationOffsetId
                        )
                    );

                    // Add the new First Destination Number update for next command
                    destinationOffsetId += deviceNumberProtectDataItemsNormalChunck[nbrCmdToSend].length;
                }
                // extended commands
                // Creates an array of deviceNumberProtectDataItemsExtendedChunck split into groups the length of the numberOfProtectTallies.
                const deviceNumberProtectDataItemsExtendedChunck = _.chunk(
                    deviceNumberProtectDataItemsExtendedSliced,
                    this.params.numberOfProtectTallies
                );
                // Gets the first time the First Destination number
                destinationOffsetId = 896;

                // Generate the List of Extended Command to send
                for (
                    let nbrCmdToSend = 0;
                    nbrCmdToSend < deviceNumberProtectDataItemsExtendedChunck.length;
                    nbrCmdToSend++
                ) {
                    // Add the command buffer to the array
                    buildDataArray.push(
                        this.buildDataExtended(
                            this.params,
                            deviceNumberProtectDataItemsExtendedChunck[nbrCmdToSend],
                            destinationOffsetId
                        )
                    );

                    // Add the new First Destination Number update for next command
                    destinationOffsetId += deviceNumberProtectDataItemsExtendedChunck[nbrCmdToSend].length;
                }
                return buildDataArray;
            }
        }
    }

    /**
     * Validate the parameters, options and items and throw a ValidationError in case of error
     *
     * @private
     * @param {ProtectTallyDumpCommandParams} params the command parameters
     * @memberof ProtectTallyDumpCommand
     */
    private validateParams(params: ProtectTallyDumpCommandParams): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
    * Builds the general command
    * Returns Probel SW-P-08 - Protect Tally Dump Message CMD_020_0X14
    *
    * + This message is issued by the Pro-Bel Controller in response to a PROTECT DUMP REQUEST (Command Byte 19).
    * + It returns all the Protect Information.
    *
    | Message    | Command Byte  | 020 - 0x14                                                                                                                         |
    |------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
    | Byte       | Field Format  | Notes                                                                                                                              |
    | Byte 1     | Matrix/Level  | Matrix/Level number as defined in 3.1.2                                                                                            |
    | Byte 2     | Num Protect   | Number of Protect Tallies (Maximum 64)                                                                                             |
    | Byte 3     | 1st Dest mult | First Destination Number DIV 256                                                                                                   |
    | Byte 4     | 1st Dest num  | First Destination Number MOD 256                                                                                                   |
    | Byte 5 & 6 | Dev and data  | First Device Number & Protect Data                                                                                                 |
    |            | Bit[15]       | Not used                                                                                                                           |
    |            | Bits[12-14]   | Protect Data                                                                                                                       |
    |            | 0             | Not Protected                                                                                                                      |
    |            | 1             | Pro-Bel Protected                                                                                                                  |
    |            | 2             | Pro-Bel override Protected (cannot be altered remotely)                                                                            |
    |            | 3             | OEM Protected                                                                                                                      |
    |            | Bits[10-11]   | Not used                                                                                                                           |
    |            | Bits[0-9]     | Device number                                                                                                                      |
    | Byte 7 & 8 |               | Second Device Number & Protect Data (As bytes 5 & 6 above)                                                                         |
    | Etc ...    |               |                                                                                                                                    |
    *
    * Use case :
    *
    * Verify if sum of deviceNumberProtectDataItems.lenght and firstDestinationId is less than 896
    * + If Yes then buildDataNormal().
    * + If No, then needs to slice the deviceNumberProtectDataItems into two arrays of deviceNumberProtectDataItems
    * - First slice for builDataNormal
    * - Second slice for buildDataExtended
    *
    * @private
    * @param {ProtectTallyDumpCommandParams} params
    * @param {ProtectTallyDumpCommandItems[]} items
    * @param {number} destinationOffsetId
    * @returns {Buffer} the command message
    * @memberof ProtectTallyDumpCommand
    */
    private buildDataNormal(
        params: ProtectTallyDumpCommandParams,
        items: ProtectTallyDumpCommandItems[],
        destinationOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            //
            (items.length - 1) * 2 + 7;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })

            .writeUInt8(CommandIdentifiers.TX.GENERAL.PROTECT_TALLY_DUMP_MESSAGE.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(params.matrixId, params.levelId))
            .writeUInt8(items.length)
            .writeUInt8(Math.floor(destinationOffsetId / 256))
            .writeUInt8(destinationOffsetId % 256);

        for (let itemIndex = 0; itemIndex < items.length; itemIndex++) {
            const data = items[itemIndex];
            buffer
                .writeUInt8((data.protectedData << 4) | Math.floor(data.deviceId / 256))
                .writeUInt8(data.deviceId % 256);
        }
        return buffer.toBuffer();
    }

    /**
    * Builds the extended command
    * Returns Probel SW-P-08 - Extended Protect Tally Dump Message CMD_148_0X94
    *
    * + This message is issued by the Pro-Bel Controller in response to an EXTENDED PROTECT DUMP REQUEST (Command Byte 147).
    * + It returns all the Protect Information.
    *
    | Message  | Command Byte   | 148 - 0x94                                                                                                                         |
    |----------|----------------|------------------------------------------------------------------------------------------------------------------------------------|
    | Byte     | Field Format   | Notes                                                                                                                              |
    | Byte 1   | Matrix number  |                                                                                                                                    |
    | Byte 2   | Level number   |                                                                                                                                    |
    | Byte 3   | Num of Protect | Number of Protect Tallies (Maximum 64)                                                                                             |
    | Byte 4   | 1st Dest mult  | First Destination number multiplier DIV 256                                                                                        |
    | Byte 5   | 1st Dest num   | First Destination number MOD 256                                                                                                   |
    | Byte 6&7 | Device/protect | First Device Number & Protect Data                                                                                                 |
    |          | Bit[15]        | Not used                                                                                                                           |
    |          | Bits[12-14]    | Protected data                                                                                                                     |
    |          | 0              | Indicates Not Protected                                                                                                            |
    |          | 1              | Indicates Pro-Bel Protected                                                                                                        |
    |          | 2              | Pro-Bel override Protected (Cannot be altered remotely)                                                                            |
    |          | 3              | Indicates OEM Protected                                                                                                            |
    |          | Bits[10-11]    | Not used                                                                                                                           |
    |          | Bits[0-9]      | Device number                                                                                                                      |
    | Byte 8&9 | Device/protect | Second Device Number & Protect Data (As bytes 6 & 7 above)                                                                         |
    | Etc ...  |                |                                                                                                                                    |
    *
    * @private
    * @param {ProtectTallyDumpCommandParams} params
    * @param {ProtectTallyDumpCommandItems[]} items
    * @param {number} destinationOffsetId
    * @returns {Buffer} the command message
    * @memberof ProtectTallyDumpCommand
    */
    private buildDataExtended(
        params: ProtectTallyDumpCommandParams,
        items: ProtectTallyDumpCommandItems[],
        destinationOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            //
            (items.length - 1) * 2 + 8;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })

            .writeUInt8(CommandIdentifiers.TX.EXTENDED.PROTECT_TALLY_DUMP_MESSAGE.id)
            .writeUInt8(params.matrixId)
            .writeUInt8(params.levelId)
            .writeUInt8(items.length)
            .writeUInt8(Math.floor(destinationOffsetId / 256))
            .writeUInt8(destinationOffsetId % 256);

        for (let itemIndex = 0; itemIndex < items.length; itemIndex++) {
            const data = items[itemIndex];
            buffer
                .writeUInt8((data.protectedData << 4) | Math.floor(data.deviceId / 256))
                .writeUInt8(data.deviceId % 256);
        }
        return buffer.toBuffer();
    }
}
